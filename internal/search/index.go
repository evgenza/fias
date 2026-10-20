package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// EnsureIndex creates the versioned index and write alias on a fresh installation.
// Existing physical indices require the explicit, offline MigrateIndex command.
func (c *Client) EnsureIndex(ctx context.Context) error {
	aliased, err := c.hasCurrentAlias(ctx)
	if err != nil || aliased {
		return err
	}
	status, err := c.request(ctx, http.MethodHead, "/"+url.PathEscape(c.index), nil, nil)
	if status != http.StatusNotFound {
		if err != nil {
			return err
		}
		return fmt.Errorf("search: legacy index %s requires offline migration: go run ./cmd/gar-search-migrate", c.index)
	}
	return c.createVersion(ctx, true)
}

func (c *Client) versionedIndex() string { return c.index + "-v2" }

func (c *Client) hasCurrentAlias(ctx context.Context) (bool, error) {
	var aliases map[string]json.RawMessage
	status, err := c.request(ctx, http.MethodGet, "/_alias/"+url.PathEscape(c.index), nil, &aliases)
	if status == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(aliases) != 1 || aliases[c.versionedIndex()] == nil {
		return false, fmt.Errorf("search: alias %s must point only to %s", c.index, c.versionedIndex())
	}
	return true, c.checkVersion(ctx)
}

func (c *Client) checkVersion(ctx context.Context) error {
	var mappings map[string]struct {
		Mappings struct {
			Meta struct {
				Version int `json:"gar_mapping_version"`
			} `json:"_meta"`
		} `json:"mappings"`
	}
	_, err := c.request(ctx, http.MethodGet, "/"+url.PathEscape(c.versionedIndex())+"/_mapping", nil, &mappings)
	if err != nil {
		return err
	}
	if mappings[c.versionedIndex()].Mappings.Meta.Version != 2 {
		return fmt.Errorf("search: %s has incompatible mapping", c.versionedIndex())
	}
	return nil
}

func (c *Client) createVersion(ctx context.Context, alias bool) error {
	var body map[string]any
	if err := json.Unmarshal([]byte(indexMapping), &body); err != nil {
		return err
	}
	if alias {
		body["aliases"] = map[string]any{c.index: map[string]any{"is_write_index": true}}
	}
	_, err := c.request(ctx, http.MethodPut, "/"+url.PathEscape(c.versionedIndex()), body, nil)
	if err != nil {
		// A previous attempt or another initializer may already have created v2.
		if checkErr := c.checkVersion(ctx); checkErr != nil {
			return err
		}
		if alias {
			current, aliasErr := c.hasCurrentAlias(ctx)
			if aliasErr != nil {
				return aliasErr
			}
			if !current {
				return fmt.Errorf("search: unfinished migration for %s; run gar-search-migrate", c.index)
			}
		}
	}
	return nil
}

// MigrateIndex runs with indexer writers stopped. It blocks legacy writes,
// copies and verifies every document, then atomically replaces the old index
// with the write alias. On failure the source remains present and write-blocked;
// rerunning this command resumes the copy into the same v2 index.
func (c *Client) MigrateIndex(ctx context.Context) error {
	current, err := c.hasCurrentAlias(ctx)
	if err != nil || current {
		return err
	}
	source := "/" + url.PathEscape(c.index)
	status, err := c.request(ctx, http.MethodHead, source, nil, nil)
	if status == http.StatusNotFound {
		return c.EnsureIndex(ctx)
	}
	if err != nil {
		return err
	}
	if _, err := c.request(ctx, http.MethodPut, source+"/_block/write", nil, nil); err != nil {
		return err
	}
	if err := c.createVersion(ctx, false); err != nil {
		return err
	}
	if _, err := c.request(ctx, http.MethodPost, source+"/_refresh", nil, nil); err != nil {
		return err
	}
	var result struct {
		TimedOut bool              `json:"timed_out"`
		Failures []json.RawMessage `json:"failures"`
	}
	body := map[string]any{"source": map[string]any{"index": c.index}, "dest": map[string]any{"index": c.versionedIndex()}}
	if _, err := c.request(ctx, http.MethodPost, "/_reindex?refresh=true&wait_for_completion=true", body, &result); err != nil {
		return err
	}
	if result.TimedOut || len(result.Failures) > 0 {
		return fmt.Errorf("search: reindex incomplete: timed_out=%t failures=%s", result.TimedOut, result.Failures)
	}
	counts := make([]int64, 2)
	for i, index := range []string{c.index, c.versionedIndex()} {
		var count struct {
			Count int64 `json:"count"`
		}
		if _, err := c.request(ctx, http.MethodGet, "/"+url.PathEscape(index)+"/_count", nil, &count); err != nil {
			return err
		}
		counts[i] = count.Count
	}
	if counts[0] != counts[1] {
		return fmt.Errorf("search: reindex count mismatch: legacy=%d v2=%d", counts[0], counts[1])
	}
	actions := map[string]any{"actions": []any{
		map[string]any{"remove_index": map[string]any{"index": c.index}},
		map[string]any{"add": map[string]any{"index": c.versionedIndex(), "alias": c.index, "is_write_index": true}},
	}}
	_, err = c.request(ctx, http.MethodPost, "/_aliases", actions, nil)
	return err
}

func (c *Client) request(ctx context.Context, method, path string, body, target any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.es.Perform(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("search: %s %s: status %d: %s", method, path, resp.StatusCode, data)
	}
	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return resp.StatusCode, err
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, nil
}
