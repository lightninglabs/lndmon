package collectors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestIsDeadlineExceeded verifies that IsDeadlineExceeded recognizes the
// various shapes a deadline error can take, most importantly a server-side
// gRPC status error whose code is DeadlineExceeded but whose description does
// not mention "context deadline exceeded".
func TestIsDeadlineExceeded(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "bare context deadline",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "grpc status with context deadline desc",
			err: status.Error(
				codes.DeadlineExceeded,
				context.DeadlineExceeded.Error(),
			),
			want: true,
		},
		{
			name: "grpc RST_STREAM deadline",
			err: status.Error(
				codes.DeadlineExceeded,
				"stream terminated by RST_STREAM with error "+
					"code: CANCEL",
			),
			want: true,
		},
		{
			name: "wrapped grpc RST_STREAM deadline",
			err: fmt.Errorf("WalletCollector WalletBalance failed "+
				"with: %w", status.Error(
				codes.DeadlineExceeded,
				"stream terminated by RST_STREAM with error "+
					"code: CANCEL",
			)),
			want: true,
		},
		{
			name: "unrelated grpc error",
			err: status.Error(
				codes.Unavailable, "connection refused",
			),
			want: false,
		},
		{
			name: "unrelated plain error",
			err:  errors.New("something else failed"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDeadlineExceeded(tc.err); got != tc.want {
				t.Fatalf("IsDeadlineExceeded(%v) = %v, want %v",
					tc.err, got, tc.want)
			}
		})
	}
}
