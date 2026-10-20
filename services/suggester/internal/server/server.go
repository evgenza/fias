// Package server — реализация gRPC-сервиса suggest.v1.Suggester поверх
// поискового индекса адресов ГАР в Elasticsearch.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gar-importer/internal/search"
	suggestv1 "gar-importer/proto/suggest/v1"
)

// Границы параметров подсказок.
const (
	// defaultLimit — размер выдачи, если limit не задан.
	defaultLimit = 10
	// maxLimit — потолок размера выдачи.
	maxLimit = 50
)

// Suggester — реализация suggest.v1.SuggesterServer.
type Suggester struct {
	suggestv1.UnimplementedSuggesterServer
	es    *elasticsearch.Client
	index string
	log   *slog.Logger
}

// New создаёт Suggester поверх клиента поискового индекса.
func New(client *search.Client, log *slog.Logger) *Suggester {
	return &Suggester{es: client.ES(), index: client.IndexName(), log: log}
}

// GetAddress возвращает адрес по object_id (term-запрос); отсутствующий
// документ — codes.NotFound.
func (s *Suggester) GetAddress(ctx context.Context, req *suggestv1.GetAddressRequest) (*suggestv1.Address, error) {
	id := strings.TrimSpace(req.GetObjectId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "object_id не задан")
	}
	query := map[string]any{
		"size":  1,
		"query": map[string]any{"term": map[string]any{"object_id": id}},
	}
	hits, err := s.searchES(ctx, query)
	if err != nil {
		return nil, s.searchError(ctx, err)
	}
	if len(hits) == 0 {
		return nil, status.Errorf(codes.NotFound, "адрес с object_id %s не найден", id)
	}
	return toProto(hits[0]), nil
}

// Suggest возвращает подсказки по подстроке адреса: prefix-совпадения по
// full_address.prefix, отсортированные по убыванию релевантности и уровню.
func (s *Suggester) Suggest(ctx context.Context, req *suggestv1.SuggestRequest) (*suggestv1.SuggestResponse, error) {
	query, limit, err := validateSuggest(req)
	if err != nil {
		return nil, err
	}
	hits, err := s.searchES(ctx, suggestQuery(query, req, limit, false, nil))
	if err != nil {
		return nil, s.searchError(ctx, err)
	}
	resp := &suggestv1.SuggestResponse{Addresses: make([]*suggestv1.Address, 0, len(hits))}
	for _, h := range hits {
		resp.Addresses = append(resp.Addresses, toProto(h))
	}
	return resp, nil
}

// StreamSuggestions стримит подсказки двумя порциями: сначала
// prefix-совпадения (как Suggest), затем, если лимит не исчерпан, —
// fuzzy-добор по full_address (fuzziness AUTO) без уже отправленных
// object_id. Клиент видит первые результаты до завершения всего поиска.
func (s *Suggester) StreamSuggestions(req *suggestv1.SuggestRequest, stream suggestv1.Suggester_StreamSuggestionsServer) error {
	query, limit, err := validateSuggest(req)
	if err != nil {
		return err
	}
	ctx := stream.Context()
	sent := map[string]struct{}{}
	delivered := 0

	// Первая порция: prefix-совпадения.
	hits, err := s.searchES(ctx, suggestQuery(query, req, limit, false, nil))
	if err != nil {
		return s.searchError(ctx, err)
	}
	for _, h := range hits {
		if err := sendAddress(ctx, stream, h); err != nil {
			return err
		}
		sent[h.Source.ObjectID] = struct{}{}
		delivered++
	}

	// Вторая порция: fuzzy-добор без уже отправленных документов.
	if delivered < limit {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		excluded := make([]string, 0, len(sent))
		for id := range sent {
			excluded = append(excluded, id)
		}
		hits, err := s.searchES(ctx, suggestQuery(query, req, limit-delivered, true, excluded))
		if err != nil {
			return s.searchError(ctx, err)
		}
		for _, h := range hits {
			if err := sendAddress(ctx, stream, h); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendAddress отправляет одно попадание в поток, контролируя отмену контекста.
func sendAddress(ctx context.Context, stream suggestv1.Suggester_StreamSuggestionsServer, h hit) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return stream.Send(toProto(h))
}

// validateSuggest проверяет запрос подсказок и возвращает нормализованные
// query (без краевых пробелов) и limit (по умолчанию 10, потолок 50).
func validateSuggest(req *suggestv1.SuggestRequest) (string, int, error) {
	query := strings.TrimSpace(req.GetQuery())
	if err := search.ValidateSuggestInput(req.GetQuery(), strings.TrimSpace(req.GetRegion())); err != nil {
		return "", 0, status.Error(codes.InvalidArgument, err.Error())
	}
	if query == "" {
		return "", 0, status.Error(codes.InvalidArgument, "query не задан")
	}
	if req.GetLimit() < 0 {
		return "", 0, status.Error(codes.InvalidArgument, "limit не может быть отрицательным")
	}
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return query, limit, nil
}

// suggestQuery собирает тело поискового запроса подсказок: must —
// prefix-совпадение по full_address.prefix (match с operator and; fuzzy=true —
// match по full_address с fuzziness AUTO), фильтры по региону и флагам
// актуальности, must_not — исключённые object_id. Сортировка: _score desc,
// level asc.
func suggestQuery(query string, req *suggestv1.SuggestRequest, size int, fuzzy bool, exclude []string) map[string]any {
	field := "full_address.prefix"
	params := map[string]any{"query": query, "operator": "and"}
	if fuzzy {
		field = "full_address"
		params = map[string]any{"query": query, "fuzziness": "AUTO"}
	}
	must := []any{map[string]any{"match": map[string]any{field: params}}}
	filter := []any{}
	if region := strings.TrimSpace(req.GetRegion()); region != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"region": region}})
	}
	if req.GetActualOnly() {
		filter = append(filter,
			map[string]any{"term": map[string]any{"is_actual": true}},
			map[string]any{"term": map[string]any{"is_active": true}},
		)
	}
	boolQuery := map[string]any{"must": must}
	if len(filter) > 0 {
		boolQuery["filter"] = filter
	}
	if len(exclude) > 0 {
		boolQuery["must_not"] = []any{map[string]any{"terms": map[string]any{"object_id": exclude}}}
	}
	return map[string]any{
		"size":         size,
		"track_scores": true,
		"sort": []any{
			map[string]any{"_score": map[string]any{"order": "desc"}},
			map[string]any{"level": map[string]any{"order": "asc"}},
		},
		"query": map[string]any{"bool": boolQuery},
	}
}

// hit — одно попадание поискового ответа Elasticsearch.
type hit struct {
	Score  *float64        `json:"_score"`
	Source search.Document `json:"_source"`
}

// searchES выполняет поисковый запрос к индексу и разбирает попадания.
func (s *Suggester) searchES(ctx context.Context, query map[string]any) ([]hit, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("suggester: кодирование запроса: %w", err)
	}
	resp, err := s.es.Search(
		s.es.Search.WithContext(ctx),
		s.es.Search.WithIndex(s.index),
		s.es.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("suggester: запрос Elasticsearch: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return nil, &esStatusError{code: resp.StatusCode}
	}
	var result struct {
		Hits struct {
			Hits []hit `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("suggester: разбор ответа Elasticsearch: %w", err)
	}
	return result.Hits.Hits, nil
}

// toProto конвертирует попадание поиска в сообщение Address.
func toProto(h hit) *suggestv1.Address {
	score := 0.0
	if h.Score != nil {
		score = *h.Score
	}
	doc := h.Source
	return &suggestv1.Address{
		ObjectId:    doc.ObjectID,
		ObjectGuid:  doc.ObjectGUID,
		Kind:        doc.Kind,
		FullAddress: doc.FullAddress,
		Name:        doc.Name,
		TypeName:    doc.TypeName,
		Region:      doc.Region,
		Level:       int32(doc.Level),
		IsActual:    doc.IsActual,
		IsActive:    doc.IsActive,
		Score:       score,
	}
}

type esStatusError struct{ code int }

func (e *esStatusError) Error() string { return fmt.Sprintf("Elasticsearch HTTP %d", e.code) }

func (s *Suggester) searchError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		s.log.InfoContext(ctx, "поиск прерван", "error", err)
		return status.FromContextError(err).Err()
	}
	var esErr *esStatusError
	if errors.As(err, &esErr) && esErr.code >= 400 && esErr.code < 500 {
		if esErr.code == 400 || esErr.code == 422 {
			return status.Error(codes.InvalidArgument, "поисковый запрос отклонён")
		}
		if esErr.code != 429 {
			s.log.ErrorContext(ctx, "ошибка конфигурации Elasticsearch", "status", esErr.code)
			return status.Error(codes.Internal, "ошибка поискового индекса")
		}
	}
	s.log.ErrorContext(ctx, "поисковый индекс недоступен", "error", err)
	return status.Error(codes.Unavailable, "поисковый индекс временно недоступен")
}
