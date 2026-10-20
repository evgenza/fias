package platform

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerLimits(t *testing.T) {
	s := newHTTPServer(":0", http.NotFoundHandler())
	if s.ReadTimeout != 15*time.Second || s.WriteTimeout != 30*time.Second || s.IdleTimeout != 120*time.Second || s.MaxHeaderBytes != 64<<10 {
		t.Fatalf("unsafe server limits: %+v", s)
	}
}
