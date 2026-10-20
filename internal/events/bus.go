package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Поток событий платформы.
const (
	// StreamName — имя JetStream-потока платформы.
	StreamName = "GAR"
	// StreamSubjects — все subject-ы платформы.
	StreamSubjects = "gar.>"
)

// Connect устанавливает соединение с NATS с автоматическими повторными
// подключениями по умолчанию.
func Connect(url string) (*nats.Conn, error) {
	conn, err := nats.Connect(url,
		nats.Name("gar-platform"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 5*time.Second),
		nats.Timeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("events: подключение к NATS %q: %w", url, err)
	}
	return conn, nil
}

// EnsureStream создаёт поток GAR или дополняет существующий subject-ами
// платформы; операция идемпотентна: при актуальной конфигурации обновления
// не происходит.
func EnsureStream(ctx context.Context, js nats.JetStreamContext) (*nats.StreamInfo, error) {
	info, err := js.StreamInfo(StreamName, nats.Context(ctx))
	switch {
	case errors.Is(err, nats.ErrStreamNotFound):
		config := &nats.StreamConfig{
			Name:      StreamName,
			Subjects:  []string{StreamSubjects},
			Retention: nats.LimitsPolicy,
			Storage:   nats.FileStorage,
			Replicas:  1,
		}
		created, err := js.AddStream(config, nats.Context(ctx))
		if err != nil {
			return nil, fmt.Errorf("events: создание потока %s: %w", StreamName, err)
		}
		return created, nil
	case err != nil:
		return nil, fmt.Errorf("events: чтение потока %s: %w", StreamName, err)
	}
	for _, subject := range info.Config.Subjects {
		if subject == StreamSubjects {
			return info, nil
		}
	}
	// У потока нет subject-ов платформы: дополняем, не трогая чужие.
	updated := info.Config
	updated.Subjects = append(append([]string{}, info.Config.Subjects...), StreamSubjects)
	info, err = js.UpdateStream(&updated, nats.Context(ctx))
	if err != nil {
		return nil, fmt.Errorf("events: обновление потока %s: %w", StreamName, err)
	}
	return info, nil
}
