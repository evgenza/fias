package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMuxErrorsAndHeaders(t *testing.T) {
	mux := newMux(&handler{})
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{
		{"GET", "/missing", "NOT_FOUND", 404}, {"POST", "/api/v1/suggest", "METHOD_NOT_ALLOWED", 405}, {"DELETE", "/api/v1/addresses/1", "METHOD_NOT_ALLOWED", 405},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		var got struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != tc.status || got.Error.Code != tc.code {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Error("missing security headers")
		}
		if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("Allow: %q", w.Header().Get("Allow"))
		}
	}
}
