//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tces "github.com/testcontainers/testcontainers-go/modules/elasticsearch"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"gar-importer/internal/config"
	"gar-importer/internal/events"
	"gar-importer/internal/gartype"
	"gar-importer/internal/migrations"
	"gar-importer/internal/search"
	"gar-importer/internal/storage"
	"gar-importer/services/indexer/internal/app"
)

// Идентификаторы посеянной цепочки: регион «г Москва» → улица «ул Тверская»
// → дом «д 7».
const (
	regionObjID = int64(1001)
	streetObjID = int64(1002)
	houseObjID  = int64(1003)
)

// Тест e2e: поднимает PostgreSQL + Elasticsearch + NATS в контейнерах,
// сее цепочку адресных объектов, запускает indexer и проверяет, что по
// событию AS_HOUSES в индексе появляется документ дома с адресной строкой
// «г Москва, ул Тверская, д 7».
func TestIndexerHousesEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	databaseURL := runPostgres(t, ctx)
	natsURL := runNATS(t, ctx)
	esAddress := runElasticsearch(t, ctx)

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
	seed(t, ctx, store)

	// Запускаем indexer в горутине.
	appCtx, stopApp := context.WithCancel(ctx)
	t.Cleanup(stopApp)
	appErr := make(chan error, 1)
	go func() {
		appErr <- app.Run(appCtx, app.Config{
			DatabaseURL: databaseURL,
			NATSURL:     natsURL,
			ESAddresses: []string{esAddress},
			ESIndex:     "gar-addresses",
			HTTPAddr:    "127.0.0.1:19212",
		})
	}()

	// Публикуем событие после запуска подписчика (durable + DeliverAllPolicy
	// гарантируют доставку и при обратном порядке).
	conn, err := events.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.EnsureStream(ctx, js); err != nil {
		t.Fatal(err)
	}
	publisher := events.NewPublisher(js)
	if err := publisher.PublishObjectsChanged(ctx, events.ObjectsChanged{
		RunID:       1,
		Kind:        "AS_HOUSES",
		Region:      77,
		Path:        "77/AS_HOUSES_20261019_1.XML",
		Written:     1,
		CompletedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	// /readyz должен перейти в 200.
	waitReady(t, ctx, "http://127.0.0.1:19212/readyz")

	// Поллим индекс до появления документа дома.
	searchClient, err := search.New(search.Config{Addresses: []string{esAddress}, Index: "gar-addresses"})
	if err != nil {
		t.Fatal(err)
	}
	doc := pollDocument(t, ctx, searchClient, strconv.FormatInt(houseObjID, 10))
	if doc.FullAddress != "г Москва, ул Тверская, д 7" {
		t.Fatalf("full_address: got %q", doc.FullAddress)
	}
	if doc.Kind != search.KindHouse {
		t.Fatalf("kind: got %q", doc.Kind)
	}
	if doc.Region != "77" {
		t.Fatalf("region: got %q", doc.Region)
	}
	if doc.Name != "7" || doc.TypeName != "д" {
		t.Fatalf("name/type_name: got %q/%q", doc.Name, doc.TypeName)
	}
	if doc.Level != 10 {
		t.Fatalf("level: got %d", doc.Level)
	}
	if !doc.IsActual || !doc.IsActive {
		t.Fatalf("flags: got actual=%v active=%v", doc.IsActual, doc.IsActive)
	}

	// Street changes must reach the existing house, without a house event.
	if _, err := store.Exec(ctx, `UPDATE gar.addr_obj SET name='Новая' WHERE object_id=$1`, streetObjID); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObjectsChanged(ctx, events.ObjectsChanged{RunID: 2, Kind: "AS_ADDR_OBJ", Region: 77, Path: "77/AS_ADDR_OBJ_20261020_1.XML", CompletedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	waitAddress(t, ctx, searchClient, "г Москва, ул Новая, д 7")
	// Registry events also refresh levels of already indexed objects.
	if _, err := store.Exec(ctx, `UPDATE gar.reestr_objects SET level_id=11 WHERE object_id=$1`, houseObjID); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObjectsChanged(ctx, events.ObjectsChanged{RunID: 3, Kind: "AS_REESTR_OBJECTS", Region: 77, Path: "77/AS_REESTR_OBJECTS_20261020_1.XML", CompletedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		latest := pollDocument(t, ctx, searchClient, strconv.FormatInt(houseObjID, 10))
		if latest.Level == 11 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registry level not refreshed: %+v", latest)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Deactivation removes the previous document after a successful empty house rebuild.
	if _, err := store.Exec(ctx, `UPDATE gar.houses SET is_active=false WHERE object_id=$1`, houseObjID); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObjectsChanged(ctx, events.ObjectsChanged{RunID: 4, Kind: "AS_HOUSES", Region: 77, Path: "77/AS_HOUSES_20261020_2.XML", CompletedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		resp, err := searchClient.ES().Get(searchClient.IndexName(), strconv.FormatInt(houseObjID, 10), searchClient.ES().Get.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("inactive house remains in Elasticsearch")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Cleanup of houses must preserve indexed address objects.
	if doc := pollDocument(t, ctx, searchClient, strconv.FormatInt(streetObjID, 10)); doc.FullAddress != "г Москва, ул Новая" {
		t.Fatalf("unrelated kind removed: %+v", doc)
	}

	// Завершаем сервис штатно.
	stopApp()
	select {
	case err := <-appErr:
		if err != nil {
			t.Fatalf("остановка indexer: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("indexer не остановился за 30 секунд")
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

func runNATS(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tcnats.Run(ctx, "nats:2.10-alpine")
	if err != nil {
		t.Skipf("docker с NATS недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера NATS: %v", err)
		}
	})
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func runElasticsearch(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tces.Run(ctx, "docker.elastic.co/elasticsearch/elasticsearch:8.15.5",
		testcontainers.WithEnv(map[string]string{
			"xpack.security.enabled": "false",
			"discovery.type":         "single-node",
			"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
		}),
	)
	if err != nil {
		t.Skipf("docker с Elasticsearch недоступен: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("остановка контейнера Elasticsearch: %v", err)
		}
	})
	return container.Settings.Address
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
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("/readyz не перешёл в 200 за 2 минуты")
}

// pollDocument ждёт появления документа в индексе (до 120 секунд).
func pollDocument(t *testing.T, ctx context.Context, client *search.Client, id string) search.Document {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.ES().Get(client.IndexName(), id, client.ES().Get.WithContext(ctx))
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("документ %s не найден", id)
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.IsError() {
			lastErr = fmt.Errorf("get %s: %s: %s", id, resp.Status(), body)
			time.Sleep(2 * time.Second)
			continue
		}
		var result struct {
			Source search.Document `json:"_source"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("разбор документа: %v", err)
		}
		return result.Source
	}
	t.Fatalf("документ %s не появился за 120 секунд: %v", id, lastErr)
	return search.Document{}
}

func ptr(v int64) *int64 { return &v }

func waitAddress(t *testing.T, ctx context.Context, client *search.Client, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		doc := pollDocument(t, ctx, client, strconv.FormatInt(houseObjID, 10))
		if doc.FullAddress == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("house still has old address: %q want %q", doc.FullAddress, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
