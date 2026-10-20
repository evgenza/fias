//go:build integration

package app_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tces "github.com/testcontainers/testcontainers-go/modules/elasticsearch"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"gar-importer/internal/search"
	suggestv1 "gar-importer/proto/suggest/v1"
	"gar-importer/services/suggester/internal/app"
)

// Тест e2e: поднимает Elasticsearch в контейнере, индексирует шесть
// документов, запускает suggester на свободных портах и проверяет gRPC-методы
// (GetAddress, Suggest, StreamSuggestions) и HTTP-эндпоинты (/readyz,
// /metrics) реальным клиентом.
func TestSuggesterEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	esAddress := runElasticsearch(t, ctx)
	searchClient, err := search.New(search.Config{Addresses: []string{esAddress}, Index: "gar-addresses"})
	if err != nil {
		t.Fatal(err)
	}
	if err := searchClient.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	seedIndex(t, ctx, searchClient)

	grpcAddr := "127.0.0.1:" + freePort(t)
	httpAddr := "127.0.0.1:" + freePort(t)
	appCtx, stopApp := context.WithCancel(ctx)
	t.Cleanup(stopApp)
	appErr := make(chan error, 1)
	go func() {
		appErr <- app.Run(appCtx, app.Config{
			ESAddresses: []string{esAddress},
			ESIndex:     "gar-addresses",
			GRPCAddr:    grpcAddr,
			HTTPAddr:    httpAddr,
		})
	}()
	waitReady(t, ctx, "http://"+httpAddr+"/readyz")

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := suggestv1.NewSuggesterClient(conn)

	t.Run("GetAddress", func(t *testing.T) {
		address, err := client.GetAddress(ctx, &suggestv1.GetAddressRequest{ObjectId: "1002"})
		if err != nil {
			t.Fatal(err)
		}
		if address.FullAddress != "г Москва, ул Тверская, д 7" {
			t.Fatalf("full_address: got %q", address.FullAddress)
		}
		if address.Kind != "house" || address.Region != "77" || address.Level != 10 {
			t.Fatalf("поля: kind=%q region=%q level=%d", address.Kind, address.Region, address.Level)
		}
	})

	t.Run("GetAddress не найден", func(t *testing.T) {
		_, err := client.GetAddress(ctx, &suggestv1.GetAddressRequest{ObjectId: "9999"})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("код: got %v, want NotFound (err=%v)", status.Code(err), err)
		}
	})

	t.Run("Suggest prefix", func(t *testing.T) {
		resp, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "твер"})
		if err != nil {
			t.Fatal(err)
		}
		if !containsID(resp.Addresses, "1002") {
			t.Fatalf("подсказки по «твер» не содержат Тверскую: %v", ids(resp.Addresses))
		}
	})

	for _, tc := range []struct{ query, id string }{
		{"тверская 7", "1002"}, {"г москва", "1001"},
		{"краснопресненская", "1007"}, {"ёлочная", "1008"}, {"елочная", "1008"},
		{"оченьдлинноеназваниеулицы", "1009"}, {"к с", "1010"},
	} {
		t.Run("address tokens "+tc.query, func(t *testing.T) {
			resp, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if !containsID(resp.Addresses, tc.id) {
				t.Fatalf("query %q: missing %s in %v", tc.query, tc.id, ids(resp.Addresses))
			}
		})
	}

	t.Run("Suggest фильтр по региону", func(t *testing.T) {
		resp, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "твер", Region: "69"})
		if err != nil {
			t.Fatal(err)
		}
		if !containsID(resp.Addresses, "1006") || containsID(resp.Addresses, "1002") {
			t.Fatalf("регион 69: got %v", ids(resp.Addresses))
		}
	})

	t.Run("Suggest actual_only", func(t *testing.T) {
		resp, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "арбат", ActualOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Addresses) != 0 {
			t.Fatalf("actual_only: got %v, want пусто", ids(resp.Addresses))
		}
		resp, err = client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "арбат"})
		if err != nil {
			t.Fatal(err)
		}
		if !containsID(resp.Addresses, "1005") {
			t.Fatalf("без actual_only не найден Арбат: %v", ids(resp.Addresses))
		}
	})

	t.Run("Suggest валидация", func(t *testing.T) {
		if _, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "  "}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("пустой query: got %v", status.Code(err))
		}
		if _, err := client.Suggest(ctx, &suggestv1.SuggestRequest{Query: "твер", Limit: -1}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("отрицательный limit: got %v", status.Code(err))
		}
	})

	t.Run("StreamSuggestions prefix", func(t *testing.T) {
		stream, err := client.StreamSuggestions(ctx, &suggestv1.SuggestRequest{Query: "моск"})
		if err != nil {
			t.Fatal(err)
		}
		var received []*suggestv1.Address
		for {
			address, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			received = append(received, address)
		}
		if len(received) == 0 {
			t.Fatal("поток по «моск» пуст")
		}
		if !containsID(received, "1001") {
			t.Fatalf("поток по «моск» не содержит Москву: %v", ids(received))
		}
	})

	t.Run("StreamSuggestions fuzzy fallback", func(t *testing.T) {
		// «тверскаа» не совпадает ни с одним prefix-ngram, документы могут
		// прийти только из fuzzy-порции.
		stream, err := client.StreamSuggestions(ctx, &suggestv1.SuggestRequest{Query: "тверскаа"})
		if err != nil {
			t.Fatal(err)
		}
		var received []*suggestv1.Address
		for {
			address, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			received = append(received, address)
		}
		if !containsID(received, "1002") {
			t.Fatalf("fuzzy по «тверскаа» не нашёл Тверскую: %v", ids(received))
		}
	})

	t.Run("HTTP readyz и metrics", func(t *testing.T) {
		resp, err := http.Get("http://" + httpAddr + "/readyz")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/readyz: got %d", resp.StatusCode)
		}
		resp, err = http.Get("http://" + httpAddr + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "grpc_requests_total") {
			t.Fatal("/metrics не содержит grpc_requests_total")
		}
	})

	// Завершаем сервис штатно.
	stopApp()
	select {
	case err := <-appErr:
		if err != nil {
			t.Fatalf("остановка suggester: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("suggester не остановился за 30 секунд")
	}
}

// seedIndex индексирует тестовые документы и обновляет индекс.
func seedIndex(t *testing.T, ctx context.Context, client *search.Client) {
	t.Helper()
	now := time.Now().UTC()
	docs := []search.Document{
		{ObjectID: "1001", ObjectGUID: "10000000-0000-0000-0000-000000000001", Kind: search.KindAddrObj,
			FullAddress: "г Москва", Name: "Москва", TypeName: "г", Region: "77", Level: 1,
			IsActual: true, IsActive: true, IndexedAt: now},
		{ObjectID: "1002", ObjectGUID: "10000000-0000-0000-0000-000000000002", Kind: search.KindHouse,
			FullAddress: "г Москва, ул Тверская, д 7", Name: "7", TypeName: "д", Region: "77", Level: 10,
			IsActual: true, IsActive: true, IndexedAt: now},
		{ObjectID: "1003", ObjectGUID: "10000000-0000-0000-0000-000000000003", Kind: search.KindApartment,
			FullAddress: "г Москва, ул Тверская, кв 10", Name: "10", TypeName: "кв", Region: "77", Level: 12,
			IsActual: true, IsActive: true, IndexedAt: now},
		{ObjectID: "1004", ObjectGUID: "10000000-0000-0000-0000-000000000004", Kind: search.KindHouse,
			FullAddress: "г Санкт-Петербург, наб реки Мойки, д 12", Name: "12", TypeName: "д", Region: "78", Level: 10,
			IsActual: true, IsActive: true, IndexedAt: now},
		// Неактуальный дом: должен отсекаться actual_only.
		{ObjectID: "1005", ObjectGUID: "10000000-0000-0000-0000-000000000005", Kind: search.KindHouse,
			FullAddress: "г Москва, ул Арбат, д 1", Name: "1", TypeName: "д", Region: "77", Level: 10,
			IsActual: false, IsActive: true, IndexedAt: now},
		{ObjectID: "1006", ObjectGUID: "10000000-0000-0000-0000-000000000006", Kind: search.KindHouse,
			FullAddress: "г Тверь, ул Советская, д 3", Name: "3", TypeName: "д", Region: "69", Level: 10,
			IsActual: true, IsActive: true, IndexedAt: now},
	}
	for _, item := range []struct{ id, address string }{
		{"1007", "г Москва, ул Краснопресненская"}, {"1008", "г Москва, ул Ёлочная"},
		{"1009", "г Москва, ул Оченьдлинноеназваниеулицы"}, {"1010", "г Москва, к 2, с 3"},
	} {
		docs = append(docs, search.Document{ObjectID: item.id, FullAddress: item.address, Region: "77", Kind: search.KindAddrObj, IsActual: true, IsActive: true, IndexedAt: now})
	}

	indexed, err := client.BulkIndex(ctx, docs)
	if err != nil {
		t.Fatal(err)
	}
	if indexed != len(docs) {
		t.Fatalf("проиндексировано %d из %d", indexed, len(docs))
	}
	// Поиск в ES near-realtime: ждём видимости документов.
	resp, err := client.ES().Indices.Refresh(
		client.ES().Indices.Refresh.WithContext(ctx),
		client.ES().Indices.Refresh.WithIndex(client.IndexName()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		t.Fatalf("refresh: %s", resp.Status())
	}
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

// freePort возвращает номер свободного TCP-порта loopback-интерфейса.
func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
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

// containsID проверяет наличие object_id в списке адресов.
func containsID(addresses []*suggestv1.Address, id string) bool {
	for _, address := range addresses {
		if address.ObjectId == id {
			return true
		}
	}
	return false
}

// ids возвращает список object_id для сообщений об ошибках.
func ids(addresses []*suggestv1.Address) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.ObjectId)
	}
	return result
}
