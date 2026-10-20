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

// EnsureIndex создаёт индекс с маппингом документов, если он отсутствует;
// существующий индекс считается актуальным (ошибка resource_already_exists
// трактуется как успех).
func (c *Client) EnsureIndex(ctx context.Context) error {
	exists, err := c.es.Indices.Exists([]string{c.index}, c.es.Indices.Exists.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("search: проверка индекса %s: %w", c.index, err)
	}
	if exists.StatusCode == 200 {
		exists.Body.Close()
		return nil
	}
	exists.Body.Close()
	resp, err := c.es.Indices.Create(c.index,
		c.es.Indices.Create.WithContext(ctx),
		c.es.Indices.Create.WithBody(strings.NewReader(indexMapping)),
	)
	if err != nil {
		return fmt.Errorf("search: создание индекса %s: %w", c.index, err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 400 && strings.Contains(string(body), "resource_already_exists_exception") {
			return nil
		}
		return fmt.Errorf("search: создание индекса %s: %s: %s", c.index, resp.Status(), strings.TrimSpace(string(body)))
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
	resp, err := c.es.Bulk(&buf, c.es.Bulk.WithContext(ctx))
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
// full_address — text (russian) с подполями prefix (edge_ngram 2..15)
// и keyword; name — text (russian) с подполем keyword.
const indexMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "max_ngram_diff": 13,
    "analysis": {
      "filter": {
        "address_edge_ngram": {
          "type": "edge_ngram",
          "min_gram": 2,
          "max_gram": 15
        }
      },
      "analyzer": {
        "address_prefix": {
          "type": "custom",
          "tokenizer": "standard",
          "filter": ["lowercase", "address_edge_ngram"]
        },
        "address_prefix_search": {
          "type": "custom",
          "tokenizer": "standard",
          "filter": ["lowercase"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "object_id": {"type": "keyword"},
      "object_guid": {"type": "keyword"},
      "kind": {"type": "keyword"},
      "full_address": {
        "type": "text",
        "analyzer": "russian",
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
        "analyzer": "russian",
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
