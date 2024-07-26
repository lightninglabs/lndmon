package collectors

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// errRPCDeadlineExceeded is the error that is sent over the gRPC
	// interface when it's coming from the server side. The
	// status.FromContextError() function won't recognize it correctly
	// since the error sent over the wire is a string and not a structured
	// error anymore.
	errRPCDeadlineExceeded = status.Error(
		codes.DeadlineExceeded, context.DeadlineExceeded.Error(),
	)
)

// IsDeadlineExceeded returns true if the passed error is a gRPC error with the
// context.DeadlineExceeded error as the cause.
func IsDeadlineExceeded(err error) bool {
	if err == nil {
		return false
	}

	st := status.FromContextError(err)
	if st.Code() == codes.DeadlineExceeded {
		return true
	}

	if strings.Contains(err.Error(), errRPCDeadlineExceeded.Error()) {
		return true
	}

	return false
}
