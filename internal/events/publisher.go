package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Параметры повторов публикации: 3 попытки с экспоненциальным backoff.
const (
	publishAttempts = 3
	publishBackoff  = 200 * time.Millisecond
)

// Publisher — издатель событий платформы: JSON-кодирование, дедупликация
// по Nats-Msg-Id, повторы с backoff.
type Publisher struct {
	js nats.JetStreamContext
}

// NewPublisher создаёт издателя поверх контекста JetStream.
func NewPublisher(js nats.JetStreamContext) *Publisher {
	return &Publisher{js: js}
}

// PublishImportRequested публикует запрос на импорт; Nats-Msg-Id = RequestID.
func (p *Publisher) PublishImportRequested(ctx context.Context, event ImportRequested) error {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = SchemaVersion1
	}
	if err := p.publish(ctx, SubjectImportRequested, event.RequestID, event); err != nil {
		return fmt.Errorf("запрос импорта %q: %w", event.RequestID, err)
	}
	return nil
}

// PublishObjectsChanged публикует уведомление о записанных объектах;
// Nats-Msg-Id = "<runID>:<path>".
func (p *Publisher) PublishObjectsChanged(ctx context.Context, event ObjectsChanged) error {
	if event.SchemaVersion == 0 {
		event.SchemaVersion = SchemaVersion1
	}
	msgID := fmt.Sprintf("%d:%s", event.RunID, event.Path)
	if err := p.publish(ctx, SubjectObjectsChanged, msgID, event); err != nil {
		return fmt.Errorf("objects changed запуска %d (%s): %w", event.RunID, event.Path, err)
	}
	return nil
}

// publish отправляет событие с повторами; Msg-Id задаётся только при
// непустом идентификаторе.
func (p *Publisher) publish(ctx context.Context, subject, msgID string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: кодирование %s: %w", subject, err)
	}
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	if msgID != "" {
		msg.Header.Set(nats.MsgIdHdr, msgID)
	}
	var lastErr error
	for attempt := range publishAttempts {
		if attempt > 0 {
			delay := publishBackoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return fmt.Errorf("events: публикация %s: %w", subject, ctx.Err())
			case <-time.After(delay):
			}
		}
		if _, err := p.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("events: публикация %s после %d попыток: %w", subject, publishAttempts, lastErr)
}
