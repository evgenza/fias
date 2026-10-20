// Package search — обёртка над go-elasticsearch/v8 для поискового индекса
// адресов ГАР: создание индекса с закреплённым маппингом, пакетная
// индексация документов и служебные запросы (ping, count).
//
// Контракт документа индекса фиксирован и разделяется с suggester-сервисом:
// _id документа — object_id (int64 как строка), схему полей см. Document.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
)

// DefaultIndex — имя индекса адресных документов по умолчанию.
const DefaultIndex = "gar-addresses"

// Виды документов (поле kind); значения — часть контракта индекса.
const (
	KindAddrObj   = "addr_obj"
	KindHouse     = "house"
	KindApartment = "apartment"
	KindRoom      = "room"
	KindStead     = "stead"
	KindCarplace  = "carplace"
)

// Параметры повторов bulk-запросов: коды, достойные повтора, и backoff.
const (
	bulkMaxRetries    = 3
	bulkRetryBaseWait = 500 * time.Millisecond
	// maxBulkErrorSamples — сколько первых ошибок элементов bulk-ответа
	// включается в текст возвращаемой ошибки.
	maxBulkErrorSamples = 10
)

// Document — поисковый документ адреса (контракт индекса gar-addresses).
type Document struct {
	// ObjectID — идентификатор объекта ГАР (int64 как строка); используется
	// как _id документа.
	ObjectID   string `json:"object_id"`
	ObjectGUID string `json:"object_guid"`
	// Kind — вид объекта: addr_obj, house, apartment, room, stead, carplace.
	Kind string `json:"kind"`
	// FullAddress — адресная строка («г Москва, ул Тверская, д 7»).
	FullAddress string `json:"full_address"`
	// Name — наименование собственного элемента («Тверская», «7», «10»).
	Name string `json:"name"`
	// TypeName — сокращение типа собственного элемента («ул», «д», «кв»).
	TypeName string `json:"type_name"`
	// Region — код региона без ведущих нулей («77»).
	Region string `json:"region"`
	// Level — уровень объекта (reestr_objects.level_id; 0, если неизвестен).
	Level    int  `json:"level"`
	IsActual bool `json:"is_actual"`
	IsActive bool `json:"is_active"`
	// IndexedAt — момент индексации документа (UTC).
	IndexedAt time.Time `json:"indexed_at"`
}

// Config — настройки клиента Elasticsearch.
type Config struct {
	// Addresses — адреса узлов Elasticsearch (http://host:9200).
	Addresses []string
	// Index — имя индекса; пустое значение заменяется на DefaultIndex.
	Index string
}

// Client — клиент поискового индекса адресов.
type Client struct {
	es    *elasticsearch.Client
	index string
}

// New создаёт клиент; bulk-запросы повторяются при 429/502/503/504
// (3 попытки, backoff 500ms×2ⁿ).
func New(cfg Config) (*Client, error) {
	if len(cfg.Addresses) == 0 {
		return nil, fmt.Errorf("search: задайте адреса Elasticsearch")
	}
	index := cfg.Index
	if index == "" {
		index = DefaultIndex
	}
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses:     cfg.Addresses,
		RetryOnStatus: []int{429, 502, 503, 504},
		MaxRetries:    bulkMaxRetries,
		RetryBackoff: func(attempt int) time.Duration {
			return bulkRetryBaseWait << (attempt - 1)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("search: клиент Elasticsearch: %w", err)
	}
	return &Client{es: es, index: index}, nil
}

// ES отдаёт низкоуровневый клиент для смежных сервисов (suggester),
// чтобы переиспользовать соединение и настройки пакета.
func (c *Client) ES() *elasticsearch.Client { return c.es }

// IndexName возвращает имя индекса документов.
func (c *Client) IndexName() string { return c.index }

// Ping проверяет доступность Elasticsearch (для проверок готовности).
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.es.Ping(c.es.Ping.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("search: ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return fmt.Errorf("search: ping: статус %s", resp.Status())
	}
	return nil
}

// BulkIndex индексирует пакет документов (действие index, _id = object_id).
// Возвращает число успешно проиндексированных документов; при частичных
// ошибках элементов возвращается ошибка с описанием первых из них.
func (c *Client) BulkIndex(ctx context.Context, docs []Document) (int, error) {
	if len(docs) == 0 {
		return 0, nil
	}
	var buf bytes.Buffer
	for _, doc := range docs {
		action := map[string]any{"index": map[string]any{"_index": c.index, "_id": doc.ObjectID}}
		if err := json.NewEncoder(&buf).Encode(action); err != nil {
			return 0, fmt.Errorf("search: кодирование действия bulk: %w", err)
		}
		if err := json.NewEncoder(&buf).Encode(doc); err != nil {
			return 0, fmt.Errorf("search: кодирование документа %s: %w", doc.ObjectID, err)
		}
	}
	resp, err := c.es.Bulk(&buf, c.es.Bulk.WithContext(ctx), c.es.Bulk.WithRequireAlias(true))
	if err != nil {
		return 0, fmt.Errorf("search: bulk-запрос: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("search: bulk-запрос: %s: %s", resp.Status(), strings.TrimSpace(string(body)))
	}
	var result struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			ID     string `json:"_id"`
			Status int    `json:"status"`
			Error  *struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("search: разбор bulk-ответа: %w", err)
	}
	indexed := 0
	var failures []string
	for _, item := range result.Items {
		for _, op := range item {
			if op.Status >= 200 && op.Status < 300 {
				indexed++
				continue
			}
			reason := resp.Status()
			if op.Error != nil {
				reason = op.Error.Type + ": " + op.Error.Reason
			}
			if len(failures) < maxBulkErrorSamples {
				failures = append(failures, fmt.Sprintf("%s (статус %d): %s", op.ID, op.Status, reason))
			}
		}
	}
	if result.Errors {
		return indexed, fmt.Errorf("search: bulk: ошибок элементов: %d из %d; первые: %s",
			len(docs)-indexed, len(docs), strings.Join(failures, "; "))
	}
	return indexed, nil
}

// Count возвращает число документов в индексе.
func (c *Client) Count(ctx context.Context) (int64, error) {
	resp, err := c.es.Count(c.es.Count.WithContext(ctx), c.es.Count.WithIndex(c.index))
	if err != nil {
		return 0, fmt.Errorf("search: count: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return 0, fmt.Errorf("search: count: статус %s", resp.Status())
	}
	var result struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("search: разбор count-ответа: %w", err)
	}
	return result.Count, nil
}

// indexMapping — маппинг индекса gar-addresses (контракт с suggester):
// full_address/name preserve address tokens without stopwords, normalize ё/е;
// prefix uses edge n-grams 1..20 and truncates query tokens to 20.
const indexMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "max_ngram_diff": 19,
    "analysis": {
      "char_filter": {"address_yo": {"type": "mapping", "mappings": ["ё => е", "Ё => Е"]}},
      "filter": {
        "address_truncate": {"type": "truncate", "length": 20},
        "address_edge_ngram": {
          "type": "edge_ngram",
          "min_gram": 1,
          "max_gram": 20
        }
      },
      "analyzer": {
        "address_text": {"type": "custom", "tokenizer": "standard", "char_filter": ["address_yo"], "filter": ["lowercase"]},
        "address_prefix": {
          "type": "custom",
          "tokenizer": "standard",
          "char_filter": ["address_yo"],
          "filter": ["lowercase", "address_edge_ngram"]
        },
        "address_prefix_search": {
          "type": "custom",
          "tokenizer": "standard",
          "char_filter": ["address_yo"],
          "filter": ["lowercase", "address_truncate"]
        }
      }
    }
  },
  "mappings": {
    "_meta": {"gar_mapping_version": 2},
    "properties": {
      "object_id": {"type": "keyword"},
      "object_guid": {"type": "keyword"},
      "kind": {"type": "keyword"},
      "full_address": {
        "type": "text",
        "analyzer": "address_text",
        "fields": {
          "prefix": {
            "type": "text",
            "analyzer": "address_prefix",
            "search_analyzer": "address_prefix_search"
          },
          "keyword": {"type": "keyword"}
        }
      },
      "name": {
        "type": "text",
        "analyzer": "address_text",
        "fields": {
          "keyword": {"type": "keyword"}
        }
      },
      "type_name": {"type": "keyword"},
      "region": {"type": "keyword"},
      "level": {"type": "integer"},
      "is_actual": {"type": "boolean"},
      "is_active": {"type": "boolean"},
      "indexed_at": {"type": "date"}
    }
  }
}`

// DeleteStale removes obsolete documents only after a successful regional kind rebuild.
// Refresh exposes all completed bulk writes before delete_by_query takes its snapshot.
func (c *Client) DeleteStale(ctx context.Context, region, kind string, started time.Time) error {
	resp, err := c.es.Indices.Refresh(c.es.Indices.Refresh.WithContext(ctx), c.es.Indices.Refresh.WithIndex(c.index))
	if err != nil {
		return fmt.Errorf("search: refresh before cleanup: %w", err)
	}
	_, readErr := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.IsError() {
		return fmt.Errorf("search: refresh before cleanup: %s", resp.Status())
	}
	query := map[string]any{"query": map[string]any{"bool": map[string]any{"filter": []any{
		map[string]any{"term": map[string]any{"region": region}},
		map[string]any{"term": map[string]any{"kind": kind}},
		map[string]any{"range": map[string]any{"indexed_at": map[string]any{"lt": started.UTC().Format(time.RFC3339Nano)}}},
	}}}}
	body, err := json.Marshal(query)
	if err != nil {
		return err
	}
	resp, err = c.es.DeleteByQuery([]string{c.index}, bytes.NewReader(body), c.es.DeleteByQuery.WithContext(ctx), c.es.DeleteByQuery.WithRefresh(true))
	if err != nil {
		return fmt.Errorf("search: cleanup: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return fmt.Errorf("search: cleanup: %s", resp.Status())
	}
	var result struct {
		TimedOut         bool              `json:"timed_out"`
		VersionConflicts int64             `json:"version_conflicts"`
		Failures         []json.RawMessage `json:"failures"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("search: cleanup response: %w", err)
	}
	if result.TimedOut || result.VersionConflicts > 0 || len(result.Failures) > 0 {
		return fmt.Errorf("search: incomplete cleanup: timeout=%t conflicts=%d failures=%d", result.TimedOut, result.VersionConflicts, len(result.Failures))
	}
	return nil
}
