//go:build integration

package events_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"

	"gar-importer/internal/events"
)

// Тест поднимает NATS в контейнере и проверяет круговой обмен обоих типов
// событий, а также идемпотентность EnsureStream.
func TestPublishSubscribeRoundTrip(t *testing.T) {
	_, js := connectJetStream(t)
	if _, err := events.EnsureStream(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	// Повторный вызов не меняет конфигурацию и не падает.
	if _, err := events.EnsureStream(context.Background(), js); err != nil {
		t.Fatalf("повторный EnsureStream: %v", err)
	}
	publisher := events.NewPublisher(js)
	subscriber := events.NewSubscriber(js)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	request := events.ImportRequested{
		SchemaVersion: events.SchemaVersion1,
		RequestID:     "req-1",
		Bucket:        "gar-archives",
		Key:           "2026-10/gar_xml.zip",
		Regions:       []int{77, 50},
		LimitPerFile:  100,
		BatchSize:     500,
		RequestedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := publisher.PublishImportRequested(ctx, request); err != nil {
		t.Fatal(err)
	}
	changed := events.ObjectsChanged{
		SchemaVersion: events.SchemaVersion1,
		RunID:         42,
		Kind:          "AS_HOUSES",
		Region:        77,
		Path:          "77/AS_HOUSES_20261019.XML",
		Written:       12345,
		CompletedAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := publisher.PublishObjectsChanged(ctx, changed); err != nil {
		t.Fatal(err)
	}

	requests, err := subscriber.SubscribeImportRequests(ctx, "test-import")
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, requests)
	if got.Payload.RequestID != request.RequestID || got.Payload.Bucket != request.Bucket ||
		got.Payload.Key != request.Key || got.Payload.BatchSize != request.BatchSize ||
		got.Payload.LimitPerFile != request.LimitPerFile ||
		!got.Payload.RequestedAt.Equal(request.RequestedAt) ||
		fmt.Sprint(got.Payload.Regions) != fmt.Sprint(request.Regions) {
		t.Fatalf("payload запроса искажён: %+v", got.Payload)
	}
	if got.Payload.SchemaVersion != events.SchemaVersion1 {
		t.Fatalf("SchemaVersion: got %d", got.Payload.SchemaVersion)
	}
	if err := got.Ack(); err != nil {
		t.Fatal(err)
	}

	changes, err := subscriber.SubscribeObjectsChanged(ctx, "test-changed")
	if err != nil {
		t.Fatal(err)
	}
	gotChanged := receive(t, changes)
	if !equalObjectsChanged(gotChanged.Payload, changed) {
		t.Fatalf("payload изменений искажён: %+v", gotChanged.Payload)
	}
	if err := gotChanged.Ack(); err != nil {
		t.Fatal(err)
	}
}

// Тест проверяет повторную доставку после Nak: то же сообщение приходит
// ещё раз и подтверждается.
func TestRedeliveryAfterNak(t *testing.T) {
	_, js := connectJetStream(t)
	if _, err := events.EnsureStream(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	publisher := events.NewPublisher(js)
	if err := publisher.PublishImportRequested(ctx, events.ImportRequested{
		RequestID: "req-nak", Bucket: "gar-archives", Key: "delta.zip", RequestedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	subscriber := events.NewSubscriber(js)
	requests, err := subscriber.SubscribeImportRequests(ctx, "test-nak")
	if err != nil {
		t.Fatal(err)
	}
	first := receive(t, requests)
	if err := first.Nak(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	second := receive(t, requests)
	if second.Payload.RequestID != "req-nak" {
		t.Fatalf("повторная доставка: got %+v", second.Payload)
	}
	meta, err := second.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.NumDelivered < 2 {
		t.Fatalf("ожидалась повторная доставка, delivered=%d", meta.NumDelivered)
	}
	if err := second.Ack(); err != nil {
		t.Fatal(err)
	}
}

// Тест проверяет дедупликацию: два publish с одним Msg-Id дают одно сообщение.
func TestDeduplicationByMsgID(t *testing.T) {
	conn, js := connectJetStream(t)
	if _, err := events.EnsureStream(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	publisher := events.NewPublisher(js)
	event := events.ImportRequested{
		RequestID: "req-dup", Bucket: "gar-archives", Key: "full.zip", RequestedAt: time.Now().UTC(),
	}
	if err := publisher.PublishImportRequested(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishImportRequested(ctx, event); err != nil {
		t.Fatal(err)
	}
	subscriber := events.NewSubscriber(js)
	requests, err := subscriber.SubscribeImportRequests(ctx, "test-dedup")
	if err != nil {
		t.Fatal(err)
	}
	first := receive(t, requests)
	if err := first.Ack(); err != nil {
		t.Fatal(err)
	}
	select {
	case duplicate := <-requests:
		t.Fatalf("дубликат дошёл до подписчика: %+v", duplicate.Payload)
	case <-time.After(5 * time.Second):
	}
}

func connectJetStream(t *testing.T) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()
	ctx := context.Background()
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
	conn, err := events.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := conn.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	return conn, js
}

// receive ждёт сообщение из канала с пределом ожидания.
func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case msg, ok := <-channel:
		if !ok {
			t.Fatal("канал подписки закрыт до получения сообщения")
		}
		return msg
	case <-time.After(30 * time.Second):
		t.Fatal("таймаут ожидания сообщения")
		var zero T
		return zero
	}
}

func equalObjectsChanged(got, want events.ObjectsChanged) bool {
	return got.SchemaVersion == want.SchemaVersion && got.RunID == want.RunID &&
		got.Kind == want.Kind && got.Region == want.Region && got.Path == want.Path &&
		got.Written == want.Written && got.CompletedAt.Equal(want.CompletedAt)
}

func TestLongProcessingAndExistingConsumer(t *testing.T) {
	_, js := connectJetStream(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := events.EnsureStream(ctx, js); err != nil {
		t.Fatal(err)
	}
	if _, err := js.AddConsumer(events.StreamName, &nats.ConsumerConfig{
		Durable: "long-processing", FilterSubject: events.SubjectImportRequested,
		AckPolicy: nats.AckExplicitPolicy, MaxDeliver: 10, BackOff: []time.Duration{time.Second},
	}); err != nil {
		t.Fatal(err)
	}
	requests, err := events.NewSubscriber(js).SubscribeImportRequests(ctx, "long-processing")
	if err != nil {
		t.Fatal(err)
	}
	info, err := js.ConsumerInfo(events.StreamName, "long-processing")
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.AckWait != 30*time.Second || len(info.Config.BackOff) != 0 {
		t.Fatalf("consumer was not migrated: %+v", info.Config)
	}
	for _, id := range []string{"long", "next"} {
		if err := events.NewPublisher(js).PublishImportRequested(ctx, events.ImportRequested{RequestID: id, Key: id}); err != nil {
			t.Fatal(err)
		}
	}
	first := receive(t, requests)
	select {
	case extra := <-requests:
		t.Fatalf("fetched while busy: %+v", extra.Payload)
	case <-time.After(35 * time.Second):
	}
	info, err = js.ConsumerInfo(events.StreamName, "long-processing")
	if err != nil {
		t.Fatal(err)
	}
	if info.Delivered.Consumer != 1 {
		t.Fatalf("deliveries while busy: %d", info.Delivered.Consumer)
	}
	if err := first.Ack(); err != nil {
		t.Fatal(err)
	}
	next := receive(t, requests)
	if next.Payload.RequestID != "next" {
		t.Fatalf("duplicate delivery: %+v", next.Payload)
	}
	if err := next.Ack(); err != nil {
		t.Fatal(err)
	}
}
