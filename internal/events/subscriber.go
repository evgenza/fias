package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"

	"gar-importer/internal/platform"
)

// Параметры консьюмера и цикла выборки.
const (
	// maxDeliver — максимум попыток доставки сообщения.
	maxDeliver = 10
	// fetchBatch — сообщений за один запрос к серверу.
	fetchBatch      = 1
	consumerAckWait = 30 * time.Second
	// fetchMaxWait — ожидание сообщений в одном запросе.
	fetchMaxWait = 2 * time.Second
	// fetchRetryMin/Max — пределы экспоненциального backoff цикла выборки
	// при недоступности сервера.
	fetchRetryMin = 100 * time.Millisecond
	fetchRetryMax = 5 * time.Second
)

// Subscriber — durable pull-подписчик на события платформы.
type Subscriber struct {
	js         nats.JetStreamContext
	terminated *prometheus.CounterVec
}

// NewSubscriber создаёт подписчика поверх контекста JetStream.
func NewSubscriber(js nats.JetStreamContext) *Subscriber {
	return &Subscriber{js: js, terminated: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gar_events_terminated_total", Help: "Messages preserved in DLQ and terminated by subject and reason.",
	}, []string{"subject", "reason"})}
}

// WithMetrics exposes subscriber termination counts in the service registry.
func (s *Subscriber) WithMetrics(registry *platform.Metrics) *Subscriber {
	registry.MustRegister(s.terminated)
	return s
}

// ImportRequestMsg — сообщение запроса импорта с распарсенным payload
// и управлением подтверждением.
type ImportRequestMsg struct {
	Payload ImportRequested
	*delivery
}

// Ack подтверждает обработку сообщения.
func (m ImportRequestMsg) Ack() error { return m.finish(func() error { return m.msg.Ack() }) }

// Nak запрашивает повторную доставку через delay.
func (m ImportRequestMsg) Nak(delay time.Duration) error {
	return m.retry(errors.New("handler requested retry"), delay)
}

// Term прекращает повторные доставки сообщения.
func (m ImportRequestMsg) Term() error {
	return m.deadLetter("permanent", errors.New("permanent handler error"))
}

// Retry retries a transient failure or preserves the final failure in DLQ.
func (m ImportRequestMsg) Retry(err error, delay time.Duration) error { return m.retry(err, delay) }

// Metadata возвращает метаданные доставки JetStream (счётчик доставок и пр.).
func (m ImportRequestMsg) Metadata() (*nats.MsgMetadata, error) { return m.msg.Metadata() }

// ObjectsChangedMsg — сообщение об изменении объектов с распарсенным payload
// и управлением подтверждением.
type ObjectsChangedMsg struct {
	Payload ObjectsChanged
	*delivery
}

// Ack подтверждает обработку сообщения.
func (m ObjectsChangedMsg) Ack() error { return m.finish(func() error { return m.msg.Ack() }) }

// Nak запрашивает повторную доставку через delay.
func (m ObjectsChangedMsg) Nak(delay time.Duration) error {
	return m.retry(errors.New("handler requested retry"), delay)
}

// Term прекращает повторные доставки сообщения.
func (m ObjectsChangedMsg) Term() error {
	return m.deadLetter("permanent", errors.New("permanent handler error"))
}

// Retry retries a transient failure or preserves the final failure in DLQ.
func (m ObjectsChangedMsg) Retry(err error, delay time.Duration) error { return m.retry(err, delay) }

// Metadata возвращает метаданные доставки JetStream (счётчик доставок и пр.).
func (m ObjectsChangedMsg) Metadata() (*nats.MsgMetadata, error) { return m.msg.Metadata() }

// SubscribeImportRequests подписывается на запросы импорта durable-консьюмером;
// канал закрывается по отмене контекста. Сообщения с битым payload
// отбрасываются (Term), чтобы не блокировать очередь.
func (s *Subscriber) SubscribeImportRequests(ctx context.Context, durable string) (<-chan ImportRequestMsg, error) {
	return subscribe(ctx, s, SubjectImportRequested, durable, func(d *delivery) (ImportRequestMsg, error) {
		var payload ImportRequested
		if err := json.Unmarshal(d.msg.Data, &payload); err != nil {
			return ImportRequestMsg{}, err
		}
		return ImportRequestMsg{Payload: payload, delivery: d}, nil
	})
}

// SubscribeObjectsChanged подписывается на уведомления об изменении объектов
// durable-консьюмером; канал закрывается по отмене контекста. Сообщения
// с битым payload отбрасываются (Term).
func (s *Subscriber) SubscribeObjectsChanged(ctx context.Context, durable string) (<-chan ObjectsChangedMsg, error) {
	return subscribe(ctx, s, SubjectObjectsChanged, durable, func(d *delivery) (ObjectsChangedMsg, error) {
		var payload ObjectsChanged
		if err := json.Unmarshal(d.msg.Data, &payload); err != nil {
			return ObjectsChangedMsg{}, err
		}
		return ObjectsChangedMsg{Payload: payload, delivery: d}, nil
	})
}

// subscribe создаёт консьюмер и запускает цикл выборки сообщений в канал.
func subscribe[T any](ctx context.Context, s *Subscriber, subject, durable string, wrap func(*delivery) (T, error)) (<-chan T, error) {
	js := s.js
	consumer := &nats.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		DeliverPolicy: nats.DeliverAllPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       consumerAckWait,
		MaxDeliver:    -1, // Application limit: DLQ failures must remain retryable.
	}
	if _, err := js.AddConsumer(StreamName, consumer, nats.Context(ctx)); err != nil {
		// Консьюмер мог остаться с прежней конфигурацией: пробуем обновить.
		if _, uerr := js.UpdateConsumer(StreamName, consumer, nats.Context(ctx)); uerr != nil {
			return nil, fmt.Errorf("events: консьюмер %q (%s): %w", durable, subject, err)
		}
	}
	subscription, err := js.PullSubscribe(subject, durable, nats.Bind(StreamName, durable))
	if err != nil {
		return nil, fmt.Errorf("events: подписка %q (%s): %w", durable, subject, err)
	}
	out := make(chan T)
	go fetchLoop(ctx, s, subscription, out, wrap)
	return out, nil
}

// fetchLoop выбирает сообщения пакетами и передаёт в канал до отмены
// контекста; при недоступности сервера — экспоненциальный backoff.
func fetchLoop[T any](ctx context.Context, s *Subscriber, subscription *nats.Subscription, out chan<- T, wrap func(*delivery) (T, error)) {
	defer close(out)
	defer func() { _ = subscription.Unsubscribe() }()
	backoff := fetchRetryMin
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := subscription.Fetch(fetchBatch, nats.MaxWait(fetchMaxWait))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				// За fetchMaxWait сообщений не пришло — штатный пустой цикл.
				continue
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, fetchRetryMax)
			continue
		}
		backoff = fetchRetryMin
		for _, msg := range msgs {
			d := &delivery{msg: msg, done: make(chan struct{}), ctx: ctx, subscriber: s}
			wrapped, err := wrap(d)
			if err != nil {
				_ = d.deadLetter("payload", err)
				continue
			}
			go d.heartbeat(ctx)
			select {
			case out <- wrapped:
			case <-ctx.Done():
				// Сообщение без Ack будет доставлено повторно по AckWait.
				return
			}
			select {
			case <-d.done:
			case <-ctx.Done():
				return
			}
		}
	}
}

// delivery holds the lease until the handler acknowledges or rejects the message.
type delivery struct {
	msg        *nats.Msg
	done       chan struct{}
	once       sync.Once
	ctx        context.Context
	subscriber *Subscriber
}

func (d *delivery) finish(action func() error) error {
	err := action()
	d.once.Do(func() { close(d.done) })
	return err
}

func (d *delivery) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(consumerAckWait / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.done:
			return
		case <-ticker.C:
			_ = d.msg.InProgress()
		}
	}
}

func (d *delivery) retry(cause error, delay time.Duration) error {
	meta, err := d.msg.Metadata()
	if err != nil {
		return d.finish(func() error { return d.msg.NakWithDelay(delay) })
	}
	if meta.NumDelivered >= maxDeliver {
		return d.deadLetter("max_deliver", cause)
	}
	return d.finish(func() error { return d.msg.NakWithDelay(delay) })
}

// Publish before Term: if DLQ is unavailable, keep the original retryable.
func (d *delivery) deadLetter(reason string, cause error) error {
	meta, err := d.msg.Metadata()
	if err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Subject    string `json:"subject"`
		RawPayload []byte `json:"raw_payload"`
		Error      string `json:"error"`
		Deliveries uint64 `json:"deliveries"`
	}{Subject: d.msg.Subject, RawPayload: d.msg.Data, Error: cause.Error(), Deliveries: meta.NumDelivered})
	if err != nil {
		return err
	}
	dlq := &nats.Msg{Subject: "gar.dlq." + d.msg.Subject, Data: data, Header: nats.Header{}}
	dlq.Header.Set(nats.MsgIdHdr, fmt.Sprintf("dlq:%s:%s:%d", meta.Stream, meta.Consumer, meta.Sequence.Stream))
	pubCtx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	defer cancel()
	if _, err := d.subscriber.js.PublishMsg(dlq, nats.Context(pubCtx)); err != nil {
		slog.Error("DLQ publication failed; original message retained", "subject", d.msg.Subject, "error", err)
		_ = d.finish(func() error { return d.msg.NakWithDelay(time.Second) })
		return err
	}
	slog.Error("message preserved in DLQ", "subject", d.msg.Subject, "reason", reason, "deliveries", meta.NumDelivered, "error", cause)
	d.subscriber.terminated.WithLabelValues(d.msg.Subject, reason).Inc()
	return d.finish(func() error { return d.msg.Term() })
}
