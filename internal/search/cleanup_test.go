package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeleteStaleScopeAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		status      int
		ok          bool
	}{
		{"success", `{"deleted":2,"failures":[]}`, 200, true},
		{"conflict", `{"version_conflicts":1}`, 200, false},
		{"timeout", `{"timed_out":true}`, 200, false},
		{"failure", `{"failures":[{"cause":"failed"}]}`, 200, false},
		{"http", `{}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refreshed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				if strings.HasSuffix(r.URL.Path, "/_refresh") {
					refreshed = true
					_, _ = w.Write([]byte(`{"_shards":{"failed":0}}`))
					return
				}
				if !refreshed {
					t.Error("delete before refresh")
				}
				if r.URL.Path != "/addresses/_delete_by_query" {
					t.Errorf("path %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				filters := body["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
				if len(filters) != 3 {
					t.Errorf("filters %v", filters)
				}
				encoded, _ := json.Marshal(body)
				for _, want := range []string{`"region":"77"`, `"kind":"house"`, `"lt":"2026-10-20T12:00:00Z"`} {
					if !strings.Contains(string(encoded), want) {
						t.Errorf("missing %s: %s", want, encoded)
					}
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.reply))
			}))
			defer server.Close()
			client, err := New(Config{Addresses: []string{server.URL}, Index: "addresses"})
			if err != nil {
				t.Fatal(err)
			}
			err = client.DeleteStale(context.Background(), "77", KindHouse, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
			if (err == nil) != tc.ok {
				t.Fatalf("error=%v want ok=%v", err, tc.ok)
			}
		})
	}
}
