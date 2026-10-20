//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"

	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/gartype"
	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
	suggestv1 "gar-importer/proto/suggest/v1"
	"gar-importer/services/address-api/internal/app"
)

// Идентификаторы посеянной цепочки: регион «г Москва» → улица «ул Тверская»
// → дом «д 7» (как в интеграционном тесте indexer).
const (
	regionObjID = int64(1001)
	streetObjID = int64(1002)
	houseObjID  = int64(1003)
)

// fakeSuggester — консервная реализация suggest.v1.Suggester для теста:
// возвращает фиксированные адреса и запоминает последний запрос.
type fakeSuggester struct {
	suggestv1.UnimplementedSuggesterServer
	mu      sync.Mutex
	lastReq *suggestv1.SuggestRequest
}

func (f *fakeSuggester) Suggest(_ context.Context, req *suggestv1.SuggestRequest) (*suggestv1.SuggestResponse, error) {
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()
	return &suggestv1.SuggestResponse{Addresses: []*suggestv1.Address{{
		ObjectId:    strconv.FormatInt(houseObjID, 10),
		ObjectGuid:  "10000000-0000-0000-0000-00000000000c",
		Kind:        "house",
		FullAddress: "г Москва, ул Тверская, д 7",
		Name:        "7",
		TypeName:    "д",
		Region:      "77",
		Level:       10,
		IsActual:    true,
		IsActive:    true,
		Score:       1.5,
	}}}, nil
}

func (f *fakeSuggester) last() *suggestv1.SuggestRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

// environment — собранный стенд: PostgreSQL + Redis + fake suggester + сервис.
type environment struct {
	baseURL     string
	store       *storage.Store
	redisClient *redis.Client
	fake        *fakeSuggester
	stopApp     context.CancelFunc
	appErr      chan error
}

// Тест e2e: поднимает PostgreSQL + Redis в контейнерах и локальный fake
// suggester, запускает address-api и проверяет REST API, кэш, статику
// и HTTP/2.
func TestAddressAPIEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := setupEnvironment(t, ctx)

	t.Run("readyz", func(t *testing.T) {
		waitReady(t, ctx, env.baseURL+"/readyz")
	})

	t.Run("suggest проксирует gRPC", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/suggest?q=%D1%82%D0%B2%D0%B5%D1%80%D1%81%D0%BA%D0%B0%D1%8F&limit=5&region=77&actual_only=true")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("suggest: статус %d", resp.StatusCode)
		}
		var body struct {
			Items []struct {
				ObjectID    string  `json:"object_id"`
				Kind        string  `json:"kind"`
				FullAddress string  `json:"full_address"`
				Score       float64 `json:"score"`
			} `json:"items"`
		}
		decode(t, resp, &body)
		if len(body.Items) != 1 {
			t.Fatalf("suggest: элементов %d, ожидался 1", len(body.Items))
		}
		item := body.Items[0]
		if item.ObjectID != "1003" || item.Kind != "house" || item.FullAddress != "г Москва, ул Тверская, д 7" || item.Score != 1.5 {
			t.Fatalf("suggest: неожиданный элемент %+v", item)
		}
		// Параметры запроса доехали до gRPC-сервиса без искажений.
		req := env.fake.last()
		if req.GetQuery() != "тверская" || req.GetLimit() != 5 || req.GetRegion() != "77" || !req.GetActualOnly() {
			t.Fatalf("suggest: suggester получил %+v", req)
		}
	})

	t.Run("suggest валидация", func(t *testing.T) {
		for _, url := range []string{
			env.baseURL + "/api/v1/suggest",
			env.baseURL + "/api/v1/suggest?q=++",
			env.baseURL + "/api/v1/suggest?q=тверская&limit=0",
			env.baseURL + "/api/v1/suggest?q=тверская&limit=51",
			env.baseURL + "/api/v1/suggest?q=тверская&actual_only=что",
		} {
			resp := get(t, ctx, url)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s: статус %d, ожидался 400", url, resp.StatusCode)
			}
			if !strings.Contains(body, `"code":"VALIDATION"`) {
				t.Fatalf("%s: ответ %s", url, body)
			}
		}
	})

	t.Run("карточка объекта и кэш", func(t *testing.T) {
		url := env.baseURL + "/api/v1/addresses/" + strconv.FormatInt(houseObjID, 10)

		resp := get(t, ctx, url)
		if resp.Header.Get("X-Cache") != "miss" {
			t.Fatalf("первый запрос: X-Cache=%q, ожидался miss", resp.Header.Get("X-Cache"))
		}
		var details struct {
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
			ParentObjectID *int64 `json:"parent_object_id"`
			ChildrenCount  *int64 `json:"children_count"`
		}
		decode(t, resp, &details)
		resp.Body.Close()

		if details.ObjectID != houseObjID || details.Kind != "house" {
			t.Fatalf("карточка: id/kind %+v", details)
		}
		if details.FullAddress != "г Москва, ул Тверская, д 7" {
			t.Fatalf("карточка: full_address %q", details.FullAddress)
		}
		if details.Name != "7" || details.TypeName != "д" || details.Level != 10 || details.Region != "77" {
			t.Fatalf("карточка: поля %+v", details)
		}
		if !details.IsActual || !details.IsActive {
			t.Fatalf("карточка: флаги actual=%v active=%v", details.IsActual, details.IsActive)
		}
		if details.ParentObjectID == nil || *details.ParentObjectID != streetObjID {
			t.Fatalf("карточка: parent_object_id %v", details.ParentObjectID)
		}
		if details.ChildrenCount == nil || *details.ChildrenCount != 0 {
			t.Fatalf("карточка: children_count %v", details.ChildrenCount)
		}

		// Повтор — из кэша; ключ реально существует в Redis.
		resp = get(t, ctx, url)
		if resp.Header.Get("X-Cache") != "hit" {
			t.Fatalf("повторный запрос: X-Cache=%q, ожидался hit", resp.Header.Get("X-Cache"))
		}
		var cached map[string]any
		decode(t, resp, &cached)
		resp.Body.Close()
		if cached["full_address"] != "г Москва, ул Тверская, д 7" {
			t.Fatalf("кэш: full_address %v", cached["full_address"])
		}
		exists, err := env.redisClient.Exists(ctx, "addr:v2:empty:"+strconv.FormatInt(houseObjID, 10)).Result()
		if err != nil {
			t.Fatalf("redis EXISTS: %v", err)
		}
		if exists != 1 {
			t.Fatal("ключа addr:v2:empty:1003 нет в Redis после запроса карточки")
		}
	})

	t.Run("неизвестный объект — 404", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/addresses/999999")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("статус %d, ожидался 404", resp.StatusCode)
		}
		if !strings.Contains(body, `"code":"NOT_FOUND"`) {
			t.Fatalf("ответ %s", body)
		}
	})

	t.Run("negative cache and import invalidation", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/addresses/999999")
		readBody(t, resp)
		if resp.StatusCode != 404 || resp.Header.Get("X-Cache") != "hit" {
			t.Fatalf("negative cache: %d %v", resp.StatusCode, resp.Header)
		}
		ttl, err := env.redisClient.TTL(ctx, "addr:v2:empty:999999").Result()
		if err != nil || ttl <= 0 || ttl > 30*time.Second {
			t.Fatalf("negative ttl %v: %v", ttl, err)
		}
		if _, err := env.store.Exec(ctx, `INSERT INTO gar_control.datasets (schema_name,scope,state) VALUES ('gar','all','ready');
 INSERT INTO gar_control.import_runs (dataset_id,mode,source_path,source_digest,state,finished_at) SELECT id,'snapshot','test','test','ready',now() FROM gar_control.datasets WHERE schema_name='gar'`); err != nil {
			t.Fatal(err)
		}
		resp = get(t, ctx, env.baseURL+"/api/v1/addresses/"+strconv.FormatInt(houseObjID, 10))
		readBody(t, resp)
		if resp.StatusCode != 200 || resp.Header.Get("X-Cache") != "miss" {
			t.Fatalf("import invalidation: %d %v", resp.StatusCode, resp.Header)
		}
	})

	t.Run("object_id не число — 400", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/addresses/abc")
		readBody(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("статус %d, ожидался 400", resp.StatusCode)
		}
	})

	t.Run("children", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/addresses/"+strconv.FormatInt(streetObjID, 10)+"/children")
		var body struct {
			Items []struct {
				ObjectID    int64  `json:"object_id"`
				Kind        string `json:"kind"`
				FullAddress string `json:"full_address"`
			} `json:"items"`
		}
		decode(t, resp, &body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("children: статус %d", resp.StatusCode)
		}
		if len(body.Items) != 1 || body.Items[0].ObjectID != houseObjID {
			t.Fatalf("children: %+v", body.Items)
		}
		if body.Items[0].Kind != "house" || body.Items[0].FullAddress != "г Москва, ул Тверская, д 7" {
			t.Fatalf("children: элемент %+v", body.Items[0])
		}

		// У несуществующего родителя — 404.
		resp = get(t, ctx, env.baseURL+"/api/v1/addresses/999999/children")
		readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("children 999999: статус %d, ожидался 404", resp.StatusCode)
		}
	})

	t.Run("карточка региона: children_count и без родителя", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/api/v1/addresses/"+strconv.FormatInt(regionObjID, 10))
		var details struct {
			Kind           string `json:"kind"`
			FullAddress    string `json:"full_address"`
			ParentObjectID *int64 `json:"parent_object_id"`
			ChildrenCount  *int64 `json:"children_count"`
		}
		decode(t, resp, &details)
		resp.Body.Close()
		if details.Kind != "addr_obj" || details.FullAddress != "г Москва" {
			t.Fatalf("регион: %+v", details)
		}
		if details.ParentObjectID != nil {
			t.Fatalf("регион: parent_object_id %v, ожидалось отсутствие", *details.ParentObjectID)
		}
		if details.ChildrenCount == nil || *details.ChildrenCount != 1 {
			t.Fatalf("регион: children_count %v, ожидалось 1", details.ChildrenCount)
		}
	})

	t.Run("openapi.yaml", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/openapi.yaml")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("статус %d", resp.StatusCode)
		}
		if !strings.Contains(body, "openapi: 3.0.3") {
			t.Fatal("openapi.yaml не содержит версию 3.0.3")
		}
	})

	t.Run("swagger ui", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/swagger/")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("статус %d", resp.StatusCode)
		}
		if !strings.Contains(body, "SwaggerUIBundle") {
			t.Fatal("/swagger/ не содержит разметку Swagger UI")
		}
		// Завендоренный ассет отдаётся из embed.
		resp = get(t, ctx, env.baseURL+"/swagger/swagger-ui.css")
		readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("swagger-ui.css: статус %d", resp.StatusCode)
		}
	})

	t.Run("веб-клиент", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("статус %d", resp.StatusCode)
		}
		if !strings.Contains(body, "ГАР address-api demo") {
			t.Fatal("/ не содержит маркер веб-клиента")
		}
		resp = get(t, ctx, env.baseURL+"/app.js")
		readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("app.js: статус %d", resp.StatusCode)
		}
	})

	t.Run("http/2 prior knowledge", func(t *testing.T) {
		// Клиент на h2c: Transport.Protocols с разрешённым незашифрованным
		// HTTP/2 (x/net/http2.Transport объявлен deprecated). HTTP/1 не
		// включаем: при обоих протоколах транспорт предпочитает HTTP/1.1.
		protocols := new(http.Protocols)
		protocols.SetUnencryptedHTTP2(true)
		client := &http.Client{Transport: &http.Transport{Protocols: protocols}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.baseURL+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("HTTP/2-запрос: %v", err)
		}
		readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("HTTP/2: статус %d", resp.StatusCode)
		}
		if resp.ProtoMajor != 2 {
			t.Fatalf("ProtoMajor=%d, ожидался 2 (h2c)", resp.ProtoMajor)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		resp := get(t, ctx, env.baseURL+"/metrics")
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("статус %d", resp.StatusCode)
		}
		for _, marker := range []string{"address_cache_total", "address_api_suggest_total", "platform_http_requests_total"} {
			if !strings.Contains(body, marker) {
				t.Fatalf("/metrics не содержит %s", marker)
			}
		}
	})

	// Штатная остановка сервиса.
	env.stopApp()
	select {
	case err := <-env.appErr:
		if err != nil {
			t.Fatalf("остановка address-api: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("address-api не остановился за 30 секунд")
	}
}

// setupEnvironment поднимает стенд и запускает сервис на свободном порту.
func setupEnvironment(t *testing.T, ctx context.Context) *environment {
	t.Helper()

	databaseURL := runPostgres(t, ctx)
	redisAddr, redisClient := runRedis(t, ctx)

	store, err := storage.Open(ctx, config.Config{
		DatabaseURL:    databaseURL,
		MaxOpenConns:   4,
		MaxIdleConns:   4,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	runner, err := migrations.NewRunner(store.SQL(), "../../../../migrations/atlas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatalf("миграции: %v", err)
	}
	if _, err := store.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS gar_control`); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Open(ctx, store.SQL()); err != nil {
		t.Fatal(err)
	}
	seed(t, ctx, store)

	fake := &fakeSuggester{}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	suggestv1.RegisterSuggesterServer(grpcServer, fake)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	t.Cleanup(grpcServer.Stop)

	httpAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	appCtx, stopApp := context.WithCancel(ctx)
	t.Cleanup(stopApp)
	appErr := make(chan error, 1)
	go func() {
		appErr <- app.Run(appCtx, app.Config{
			DatabaseURL:       databaseURL,
			SuggesterGRPCAddr: grpcListener.Addr().String(),
			RedisAddr:         redisAddr,
			CacheTTL:          time.Minute,
			HTTPAddr:          httpAddr,
		})
	}()

	return &environment{
		baseURL:     "http://" + httpAddr,
		store:       store,
		redisClient: redisClient,
		fake:        fake,
		stopApp:     stopApp,
		appErr:      appErr,
	}
}

// seed записывает цепочку «г Москва» → «ул Тверская» → «д 7» вместе
// с иерархией, реестром и справочником типов домов.
func seed(t *testing.T, ctx context.Context, store *storage.Store) {
	t.Helper()
	client := store.Client
	date := gartype.Date("2026-10-19")
	endDate := gartype.Date("2079-06-06")

	if _, err := client.HouseType.Create().
		SetID(1).SetName("дом").SetShortName("д").
		SetUpdateDate(date).SetStartDate(date).SetEndDate(endDate).
		SetIsActive(true).
		Save(ctx); err != nil {
		t.Fatalf("house_types: %v", err)
	}

	objects := []struct {
		id       int64
		guid     string
		name     string
		typeName string
		level    string
	}{
		{regionObjID, "10000000-0000-0000-0000-00000000000a", "Москва", "г", "1"},
		{streetObjID, "10000000-0000-0000-0000-00000000000b", "Тверская", "ул", "8"},
	}
	for _, obj := range objects {
		if _, err := client.AddrObj.Create().
			SetID(obj.id).SetObjectID(obj.id).SetObjectGUID(obj.guid).
			SetChangeID(1).SetName(obj.name).SetTypeName(obj.typeName).
			SetLevel(obj.level).SetOpertypeID(gartype.Integer("1")).
			SetUpdateDate(date).SetStartDate(date).SetEndDate(endDate).
			SetIsActual(true).SetIsActive(true).
			Save(ctx); err != nil {
			t.Fatalf("addr_obj %d: %v", obj.id, err)
		}
	}

	hierarchies := []struct {
		id       int64
		objectID int64
		parent   *int64
		path     string
	}{
		{1, regionObjID, nil, "1001"},
		{2, streetObjID, ptr(regionObjID), "1001.1002"},
		{3, houseObjID, ptr(streetObjID), "1001.1002.1003"},
	}
	for _, item := range hierarchies {
		create := client.AdmHierarchyItem.Create().
			SetID(item.id).SetObjectID(item.objectID).
			SetChangeID(1).SetRegionCode("77").
			SetUpdateDate(date).SetStartDate(date).SetEndDate(endDate).
			SetIsActive(true).SetPath(item.path)
		if item.parent != nil {
			create.SetParentObjID(*item.parent)
		}
		if _, err := create.Save(ctx); err != nil {
			t.Fatalf("adm_hierarchy %d: %v", item.objectID, err)
		}
	}

	if _, err := client.House.Create().
		SetID(houseObjID).SetObjectID(houseObjID).
		SetObjectGUID("10000000-0000-0000-0000-00000000000c").
		SetChangeID(1).SetHouseNum("7").SetHouseType(gartype.Integer("1")).
		SetOpertypeID(gartype.Integer("1")).
		SetUpdateDate(date).SetStartDate(date).SetEndDate(endDate).
		SetIsActual(true).SetIsActive(true).
		Save(ctx); err != nil {
		t.Fatalf("houses: %v", err)
	}

	reestr := []struct {
		objectID int64
		levelID  int64
	}{
		{regionObjID, 1},
		{streetObjID, 8},
		{houseObjID, 10},
	}
	for _, entry := range reestr {
		if _, err := client.ReestrObject.Create().
			SetID(entry.objectID).SetCreateDate(date).SetChangeID(1).
			SetLevelID(entry.levelID).SetUpdateDate(date).
			SetObjectGUID(fmt.Sprintf("10000000-0000-0000-0000-%012x", entry.objectID)).
			SetIsActive(true).
			Save(ctx); err != nil {
			t.Fatalf("reestr_objects %d: %v", entry.objectID, err)
		}
	}
}

func runPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tcpostgres.Run(ctx, "postgres:18.6-alpine",
		tcpostgres.WithDatabase("gar"),
		tcpostgres.WithUsername("gar"),
		tcpostgres.WithPassword("gar"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		t.Skipf("docker с PostgreSQL недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера PostgreSQL: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// runRedis поднимает Redis generic-контейнером (модуля redis нет в go.mod).
func runRedis(t *testing.T, ctx context.Context) (string, *redis.Client) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("docker с Redis недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера Redis: %v", err)
		}
	})
	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{Addr: endpoint})
	t.Cleanup(func() { client.Close() })
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	return endpoint, client
}

// freePort резервирует свободный порт loopback.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// waitReady поллим /readyz до кода 200.
func waitReady(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			t.Logf("readyz: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("/readyz не перешёл в 200 за 2 минуты")
}

func get(t *testing.T, ctx context.Context, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func decode(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
}

func ptr(v int64) *int64 { return &v }
