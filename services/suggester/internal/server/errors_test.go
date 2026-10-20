package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSearchErrors(t *testing.T) {
	s := &Suggester{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{&esStatusError{400}, codes.InvalidArgument}, {&esStatusError{401}, codes.Internal}, {&esStatusError{404}, codes.Internal}, {&esStatusError{429}, codes.Unavailable}, {&esStatusError{503}, codes.Unavailable}, {fmt.Errorf("request: %w", context.Canceled), codes.Canceled}, {fmt.Errorf("request: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
	} {
		if got := status.Code(s.searchError(context.Background(), tc.err)); got != tc.code {
			t.Errorf("%v: %v want %v", tc.err, got, tc.code)
		}
	}
	if status.Code(normalizeContextError(context.Canceled)) != codes.Canceled {
		t.Fatal("canceled metrics code")
	}
}
