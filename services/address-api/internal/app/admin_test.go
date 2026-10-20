package app

import (
	"net/http/httptest"
	"testing"
)

func TestOperationalRoutesArePrivate(t *testing.T) {
	h := &handler{}
	public := newMux(h)
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		w := httptest.NewRecorder()
		public.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("public %s: %d", path, w.Code)
		}
	}
}
