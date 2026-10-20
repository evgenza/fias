package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gar-importer/internal/address"
	"gar-importer/internal/search"
	"gar-importer/internal/storage"
	suggestv1 "gar-importer/proto/suggest/v1"
)

// Границы параметров запросов.
const (
	defaultSuggestLimit  = 10
	maxSuggestLimit      = 50
	defaultChildrenLimit = 50
	maxChildrenLimit     = 200
	// suggestRPCTimeout — верхняя граница длительности вызова Suggester.Suggest.
	suggestRPCTimeout = 3 * time.Second
)

// Коды ошибок API (значения — часть контракта, см. api/openapi/address-api.yaml).
const (
	codeValidation  = "VALIDATION"
	codeNotFound    = "NOT_FOUND"
	codeUnavailable = "UNAVAILABLE"
	codeInternal    = "INTERNAL"
)

// headerCache — заголовок с источником ответа карточки (hit|miss).
const headerCache = "X-Cache"

// suggestItem — элемент выдачи подсказок: поля suggest.v1.Address.
type suggestItem struct {
	ObjectID    string  `json:"object_id"`
	ObjectGUID  string  `json:"object_guid"`
	Kind        string  `json:"kind"`
	FullAddress string  `json:"full_address"`
	Name        string  `json:"name"`
	TypeName    string  `json:"type_name"`
	Region      string  `json:"region"`
	Level       int32   `json:"level"`
	IsActual    bool    `json:"is_actual"`
	IsActive    bool    `json:"is_active"`
	Score       float64 `json:"score"`
}

// addressDetails — карточка адресного объекта. ChildrenCount заполняется
// только в ответе карточки; в списке дочерних объектов опускается.
type addressDetails struct {
	ObjectID       int64  `json:"object_id"`
	ObjectGUID     string `json:"object_guid"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	TypeName       string `json:"type_name"`
	Level          int    `json:"level"`
	Region         string `json:"region"`
	FullAddress    string `json:"full_address"`
	IsActual       bool   `json:"is_actual"`
	IsActive       bool   `json:"is_active"`
	ParentObjectID *int64 `json:"parent_object_id,omitempty"`
	ChildrenCount  *int64 `json:"children_count,omitempty"`
}

// handler — HTTP-обработчики API поверх PostgreSQL, Redis и gRPC suggester.
type handler struct {
	store     *storage.Store
	builder   *address.Builder
	cache     *cache
	suggester suggestv1.SuggesterClient
	metrics   *metrics
	log       *slog.Logger
}

// handleSuggest — GET /api/v1/suggest: проксирование Suggester.Suggest.
func (h *handler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if err := search.ValidateSuggestInput(r.URL.Query().Get("q"), strings.TrimSpace(r.URL.Query().Get("region"))); err != nil {
		h.metrics.suggest.WithLabelValues("invalid").Inc()
		writeError(w, http.StatusBadRequest, codeValidation, err.Error())
		return
	}
	if query == "" {
		h.metrics.suggest.WithLabelValues("invalid").Inc()
		writeError(w, http.StatusBadRequest, codeValidation, "параметр q не задан")
		return
	}
	limit, ok := parseLimit(w, r, "limit", defaultSuggestLimit, maxSuggestLimit)
	if !ok {
		h.metrics.suggest.WithLabelValues("invalid").Inc()
		return
	}
	actualOnly, ok := parseBool(w, r, "actual_only")
	if !ok {
		h.metrics.suggest.WithLabelValues("invalid").Inc()
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), suggestRPCTimeout)
	defer cancel()
	resp, err := h.suggester.Suggest(ctx, &suggestv1.SuggestRequest{
		Query:      query,
		Limit:      int32(limit),
		Region:     strings.TrimSpace(r.URL.Query().Get("region")),
		ActualOnly: actualOnly,
	})
	if err != nil {
		if h.writeContextError(w, r, err) {
			return
		}
		switch status.Code(err) {
		case codes.InvalidArgument:
			h.metrics.suggest.WithLabelValues("invalid").Inc()
			writeError(w, http.StatusBadRequest, codeValidation, "suggester отклонил запрос")
		case codes.Unavailable:
			h.metrics.suggest.WithLabelValues("unavailable").Inc()
			writeError(w, http.StatusServiceUnavailable, codeUnavailable, "сервис подсказок временно недоступен")
		default:
			h.metrics.suggest.WithLabelValues("error").Inc()
			h.log.ErrorContext(ctx, "suggest: вызов suggester не удался", "error", err)
			writeError(w, http.StatusInternalServerError, codeInternal, "внутренняя ошибка")
		}
		return
	}
	h.metrics.suggest.WithLabelValues("ok").Inc()

	items := make([]suggestItem, 0, len(resp.GetAddresses()))
	for _, addr := range resp.GetAddresses() {
		items = append(items, suggestItem{
			ObjectID:    addr.GetObjectId(),
			ObjectGUID:  addr.GetObjectGuid(),
			Kind:        addr.GetKind(),
			FullAddress: addr.GetFullAddress(),
			Name:        addr.GetName(),
			TypeName:    addr.GetTypeName(),
			Region:      addr.GetRegion(),
			Level:       addr.GetLevel(),
			IsActual:    addr.GetIsActual(),
			IsActive:    addr.GetIsActive(),
			Score:       addr.GetScore(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleAddress — GET /api/v1/addresses/{id}: карточка объекта с кэшем Redis.
func (h *handler) handleAddress(w http.ResponseWriter, r *http.Request) {
	objectID, ok := parseObjectID(w, r)
	if !ok {
		return
	}
	version := ""
	cacheable := true
	if h.cache.version != nil {
		var err error
		version, err = h.cache.version(r.Context())
		if err != nil {
			cacheable = false
			h.log.WarnContext(r.Context(), "версия кэша недоступна", "error", err)
		}
	}
	if cacheable {
		if data, hit := h.cache.get(r.Context(), objectID, version); hit {
			w.Header().Set(headerCache, "hit")
			if string(data) == "null" {
				h.writeDescribeError(w, r, objectID, address.ErrNotFound)
				return
			}
			writeRawJSON(w, http.StatusOK, data)
			return
		}
	}
	load := func(ctx context.Context) ([]byte, error) {
		details, err := h.describeWithChildren(ctx, objectID, true)
		if errors.Is(err, address.ErrNotFound) {
			return []byte("null"), nil
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(details)
	}
	result := h.cache.flights.DoChan(versionedKey(objectID, []string{version}), func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
		defer cancel()
		data, err := load(ctx)
		if err == nil && cacheable {
			h.cache.set(ctx, objectID, data, version)
		}
		return data, err
	})
	select {
	case <-r.Context().Done():
		h.writeDescribeError(w, r, objectID, r.Context().Err())
		<-result // Keep dependencies alive until the shared load and cache write finish.
	case res := <-result:
		if res.Err != nil {
			h.writeDescribeError(w, r, objectID, res.Err)
			return
		}
		data := res.Val.([]byte)
		w.Header().Set(headerCache, "miss")
		if string(data) == "null" {
			h.writeDescribeError(w, r, objectID, address.ErrNotFound)
			return
		}
		writeRawJSON(w, http.StatusOK, data)
	}

}

// handleChildren — GET /api/v1/addresses/{id}/children: активные дети иерархии.
func (h *handler) handleChildren(w http.ResponseWriter, r *http.Request) {
	objectID, ok := parseObjectID(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r, "limit", defaultChildrenLimit, maxChildrenLimit)
	if !ok {
		return
	}
	exists, err := address.ObjectExists(r.Context(), h.store, objectID)
	if err != nil {
		h.writeDescribeError(w, r, objectID, err)
		return
	}
	if !exists {
		h.writeDescribeError(w, r, objectID, address.ErrNotFound)
		return
	}
	ids, err := address.ListChildren(r.Context(), h.store, objectID, limit)
	if err != nil {
		h.log.ErrorContext(r.Context(), "children: выборка", "object_id", objectID, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "внутренняя ошибка")
		return
	}
	items := make([]addressDetails, 0, len(ids))
	descriptions, err := h.builder.DescribeObjects(r.Context(), ids)
	if err != nil {
		h.writeDescribeError(w, r, objectID, err)
		return
	}
	for _, desc := range descriptions {
		items = append(items, *detailsFromDescription(desc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// describeWithChildren собирает карточку объекта; withCount — включать
// число дочерних объектов (для ответа карточки, не для элементов списка).
func (h *handler) describeWithChildren(ctx context.Context, objectID int64, withCount bool) (*addressDetails, error) {
	desc, err := h.builder.DescribeObject(ctx, objectID)
	if err != nil {
		return nil, err
	}
	details := detailsFromDescription(desc)
	if withCount {
		count, err := address.CountChildren(ctx, h.store, objectID)
		if err != nil {
			return nil, err
		}
		details.ChildrenCount = &count
	}
	return details, nil
}

// writeDescribeError отображает ошибку DescribeObject на коды API.
func (h *handler) writeDescribeError(w http.ResponseWriter, r *http.Request, objectID int64, err error) {
	if h.writeContextError(w, r, err) {
		return
	}
	if errors.Is(err, address.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, fmt.Sprintf("адресный объект %d не найден", objectID))
		return
	}
	h.log.ErrorContext(r.Context(), "describe: запрос к PostgreSQL", "object_id", objectID, "error", err)
	writeError(w, http.StatusInternalServerError, codeInternal, "внутренняя ошибка")
}

// parseObjectID читает {id} пути как положительный int64.
func parseObjectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	objectID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || objectID <= 0 {
		writeError(w, http.StatusBadRequest, codeValidation, "object_id должен быть положительным целым")
		return 0, false
	}
	return objectID, true
}

// parseLimit читает целочисленный параметр запроса в границах 1..max
// (пусто — defaultLimit).
func parseLimit(w http.ResponseWriter, r *http.Request, name string, defaultLimit, maxLimit int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		writeError(w, http.StatusBadRequest, codeValidation,
			fmt.Sprintf("параметр %s должен быть целым от 1 до %d", name, maxLimit))
		return 0, false
	}
	return limit, true
}

// parseBool читает булев параметр запроса (пусто — false).
func parseBool(w http.ResponseWriter, r *http.Request, name string) (bool, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeValidation,
			fmt.Sprintf("параметр %s должен быть булевым (true/false)", name))
		return false, false
	}
	return value, true
}

// writeJSON пишет JSON-ответ UTF-8.
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = w.Write(data)
}

// writeRawJSON пишет уже сериализованный JSON (значение из кэша).
func writeRawJSON(w http.ResponseWriter, code int, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

// writeError пишет ошибку в формате {"error": {"code", "message"}}.
func writeError(w http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(w, statusCode, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func detailsFromDescription(desc *address.Description) *addressDetails {
	return &addressDetails{
		ObjectID:       desc.ObjectID,
		ObjectGUID:     desc.ObjectGUID,
		Kind:           desc.Kind,
		Name:           desc.Name,
		TypeName:       desc.TypeName,
		Level:          desc.Level,
		Region:         desc.Region,
		FullAddress:    desc.FullAddress,
		IsActual:       desc.IsActual,
		IsActive:       desc.IsActive,
		ParentObjectID: desc.ParentObjectID,
	}
}

func (h *handler) writeContextError(w http.ResponseWriter, r *http.Request, err error) bool {
	code := status.Code(err)
	switch {
	case errors.Is(err, context.Canceled) || code == codes.Canceled:
		h.log.InfoContext(r.Context(), "запрос отменён")
		writeError(w, 499, "CANCELED", "запрос отменён")
	case errors.Is(err, context.DeadlineExceeded) || code == codes.DeadlineExceeded:
		h.log.InfoContext(r.Context(), "превышено время запроса")
		writeError(w, http.StatusGatewayTimeout, "TIMEOUT", "превышено время запроса")
	default:
		return false
	}
	return true
}
