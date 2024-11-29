package appmetrics

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

const (
	FailureReasonKey = "failure_reason"

	LoginInvalidPayload     = "invalid_payload"
	LoginInvalidCredentials = "invalid_credentials"
)

var onceLoginSuccessCtr sync.Once
var loginSuccessCtr metric.Int64Counter = &noop.Int64Counter{}

// LoginSuccessCounter is a counter metrics to count how many successful login.
func LoginSuccessCounter(ctx context.Context, serviceName string) metric.Int64Counter {
	onceLoginSuccessCtr.Do(func() {
		var err error
		loginSuccessCtr, err = otel.Meter(instrumentationName).Int64Counter(serviceName + ".login.success")
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create login.success counter", slog.Any("error", err))
			loginSuccessCtr = &noop.Int64Counter{}
		}
	})

	return loginSuccessCtr
}

var onceloginFailureCtr sync.Once
var loginFailureCtr metric.Int64Counter = &noop.Int64Counter{}

// LoginFailureCounter is a counter metrics to count how many failed login.
func LoginFailureCounter(ctx context.Context, serviceName string) metric.Int64Counter {
	onceloginFailureCtr.Do(func() {
		var err error
		loginFailureCtr, err = otel.Meter(instrumentationName).Int64Counter(serviceName + ".login.failure")
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create login.failure counter", slog.Any("error", err))
			loginFailureCtr = &noop.Int64Counter{}
		}
	})

	return loginFailureCtr
}
