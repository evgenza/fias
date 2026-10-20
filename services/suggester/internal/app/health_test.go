package app

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestHealthFollowsSearch(t *testing.T) {
	h := health.NewServer()
	for _, tc := range []struct {
		err  error
		want healthpb.HealthCheckResponse_ServingStatus
	}{
		{errors.New("ES down"), healthpb.HealthCheckResponse_NOT_SERVING}, {nil, healthpb.HealthCheckResponse_SERVING},
	} {
		updateHealth(context.Background(), h, func(context.Context) error { return tc.err })
		for _, service := range []string{"", healthServiceName} {
			got, err := h.Check(context.Background(), &healthpb.HealthCheckRequest{Service: service})
			if err != nil || got.Status != tc.want {
				t.Fatalf("%s health %v %v", service, got, err)
			}
		}
	}
	h.Shutdown()
	updateHealth(context.Background(), h, func(context.Context) error { return nil })
	got, _ := h.Check(context.Background(), &healthpb.HealthCheckRequest{})
	if got.Status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatal("shutdown health reverted")
	}
}
