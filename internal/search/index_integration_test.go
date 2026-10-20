//go:build integration

package search

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tces "github.com/testcontainers/testcontainers-go/modules/elasticsearch"
)

func TestVersionedIndexMigrationAndAliasGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := tces.Run(ctx, "docker.elastic.co/elasticsearch/elasticsearch:8.15.5", testcontainers.WithEnv(map[string]string{
		"xpack.security.enabled": "false", "discovery.type": "single-node", "ES_JAVA_OPTS": "-Xms512m -Xmx512m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Log(err)
		}
	})
	for _, name := range []string{"legacy", "failed-copy"} {
		t.Run(name, func(t *testing.T) {
			client, err := New(Config{Addresses: []string{container.Settings.Address}, Index: name})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.request(ctx, http.MethodPut, "/"+name, map[string]any{"settings": map[string]any{"number_of_shards": 1, "number_of_replicas": 0}}, nil); err != nil {
				t.Fatal(err)
			}
			doc := map[string]any{"object_id": "1", "full_address": "г Москва, ул Ёлочная, д 7", "level": 1}
			if name == "failed-copy" {
				doc["level"] = "invalid"
			}
			if _, err := client.request(ctx, http.MethodPut, "/"+name+"/_doc/1", doc, nil); err != nil {
				t.Fatal(err)
			}
			if err := client.EnsureIndex(ctx); err == nil || !strings.Contains(err.Error(), "migration") {
				t.Fatalf("legacy accepted without explicit migration: %v", err)
			}
			if name == "failed-copy" {
				if err := client.MigrateIndex(ctx); err == nil {
					t.Fatal("expected reindex failure")
				}
				if _, err := client.request(ctx, http.MethodGet, "/"+name+"/_doc/1", nil, nil); err != nil {
					t.Fatalf("source removed after failed copy: %v", err)
				}
				if _, err := client.request(ctx, http.MethodPut, "/"+name+"/_settings", map[string]any{"index.blocks.write": false}, nil); err != nil {
					t.Fatal(err)
				}
				doc["level"] = 1
				if _, err := client.request(ctx, http.MethodPut, "/"+name+"/_doc/1", doc, nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := client.MigrateIndex(ctx); err != nil {
				t.Fatal(err)
			}
			if err := client.EnsureIndex(ctx); err != nil {
				t.Fatal(err)
			}
			if err := client.MigrateIndex(ctx); err != nil {
				t.Fatalf("migration is not idempotent: %v", err)
			}
			count, err := client.Count(ctx)
			if err != nil || count != 1 {
				t.Fatalf("migrated count=%d error=%v", count, err)
			}
			var result struct {
				Hits struct {
					Total struct {
						Value int `json:"value"`
					} `json:"total"`
				} `json:"hits"`
			}
			query := map[string]any{"query": map[string]any{"match": map[string]any{"full_address.prefix": map[string]any{"query": "г елочная 7", "operator": "and"}}}}
			if _, err := client.request(ctx, http.MethodPost, "/"+name+"/_search", query, &result); err != nil {
				t.Fatal(err)
			}
			if result.Hits.Total.Value != 1 {
				t.Fatalf("migrated mapping did not reanalyze source: %+v", result)
			}
			if _, err := client.BulkIndex(ctx, []Document{{ObjectID: "2", FullAddress: "г Москва"}}); err != nil {
				t.Fatal(err)
			}
			physical, err := New(Config{Addresses: []string{container.Settings.Address}, Index: name + "-v2"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := physical.BulkIndex(ctx, []Document{{ObjectID: "3"}}); err == nil {
				t.Fatal("bulk accepted physical index despite require_alias")
			}
		})
	}
	missing, err := New(Config{Addresses: []string{container.Settings.Address}, Index: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.BulkIndex(ctx, []Document{{ObjectID: "1"}}); err == nil {
		t.Fatal("bulk auto-created missing alias")
	}
	if status, _ := missing.request(ctx, http.MethodHead, "/missing", nil, nil); status != http.StatusNotFound {
		t.Fatalf("bulk created dynamic index, status=%d", status)
	}
	if err := missing.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if err := missing.EnsureIndex(ctx); err != nil {
		t.Fatal(err)
	}
	var analyzed struct {
		Tokens []struct {
			Token string `json:"token"`
		} `json:"tokens"`
	}
	if _, err := missing.request(ctx, http.MethodPost, "/missing/_analyze", map[string]any{"analyzer": "address_text", "text": "к с Ёлочная"}, &analyzed); err != nil {
		t.Fatal(err)
	}
	if len(analyzed.Tokens) != 3 || analyzed.Tokens[0].Token != "к" || analyzed.Tokens[1].Token != "с" || analyzed.Tokens[2].Token != "елочная" {
		t.Fatalf("address analyzer removed or changed address tokens: %+v", analyzed)
	}

}
