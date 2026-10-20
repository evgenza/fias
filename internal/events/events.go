// Package events — шина событий платформы ГАР на NATS JetStream: схемы
// событий, поток GAR, издатель с дедупликацией и durable pull-подписчики.
package events

import "time"

// SchemaVersion1 — первая версия схем событий.
const SchemaVersion1 = 1

// Subject-ы событий платформы внутри потока GAR.
const (
	// SubjectImportRequested — запрос на импорт архива ГАР из хранилища.
	SubjectImportRequested = "gar.import.requested"
	// SubjectObjectsChanged — уведомление о записанных объектах импорта.
	SubjectObjectsChanged = "gar.objects.changed"
)

// ImportRequested — запрос на импорт архива ГАР из объектного хранилища.
type ImportRequested struct {
	SchemaVersion int       `json:"schema_version"`
	RequestID     string    `json:"request_id"`
	Bucket        string    `json:"bucket"`
	Key           string    `json:"key"`
	Regions       []int     `json:"regions,omitempty"`
	LimitPerFile  int64     `json:"limit_per_file,omitempty"`
	BatchSize     int       `json:"batch_size,omitempty"`
	RequestedAt   time.Time `json:"requested_at"`
}

// ObjectsChanged — уведомление о записанных объектах по файлу плана импорта.
type ObjectsChanged struct {
	SchemaVersion int       `json:"schema_version"`
	Digest        string    `json:"digest,omitempty"`
	RunID         int64     `json:"run_id"`
	Kind          string    `json:"kind"`
	Region        int       `json:"region"`
	Path          string    `json:"path"`
	Written       int64     `json:"written"`
	CompletedAt   time.Time `json:"completed_at"`
}
