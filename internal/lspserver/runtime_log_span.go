package lspserver

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"
)

type runtimeLogSpanKey struct{}

type runtimeLogSpan struct {
	id       string
	parentID string
	traceID  string
}

var runtimeLogSpanSequence atomic.Uint64
var runtimeLogSession = strconv.FormatInt(time.Now().UnixNano(), 36)

func newRuntimeLogSpan(ctx context.Context) (context.Context, runtimeLogSpan) {
	if ctx == nil {
		ctx = context.Background()
	}
	span := runtimeLogSpan{id: runtimeLogSession + "-" + strconv.FormatUint(runtimeLogSpanSequence.Add(1), 36)}
	span.traceID = span.id
	if parent, ok := ctx.Value(runtimeLogSpanKey{}).(runtimeLogSpan); ok {
		span.parentID = parent.id
		span.traceID = parent.traceID
	}
	return context.WithValue(ctx, runtimeLogSpanKey{}, span), span
}

func (span runtimeLogSpan) addFields(fields map[string]any) {
	fields["spanId"] = span.id
	fields["traceId"] = span.traceID
	if span.parentID != "" {
		fields["parentSpanId"] = span.parentID
	}
}
