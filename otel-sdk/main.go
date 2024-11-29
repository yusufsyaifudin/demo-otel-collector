package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	// Go-Chi Router and OpenTelemetry HTTP Middleware
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	// OpenTelemetry SDK
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	// OpenTelemetry Traces
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	otelTraceNoop "go.opentelemetry.io/otel/trace/noop"

	// OpenTelemetry Metrics
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/metric"
	otelMetricNoop "go.opentelemetry.io/otel/metric/noop"
	otelSdkMetric "go.opentelemetry.io/otel/sdk/metric"

	// Internal package
	"github.com/yusufsyaifudin/demo-otel-collector/otel-sdk/pkg/appmetrics"
)

const instrumentationName = "github.com/yusufsyaifudin/demo-otel-collector/otel-sdk/main.go"

func main() {
	var (
		Port = os.Getenv("PORT")

		// OpenTemeletryHTTPEndpoint contains OpenTelemetry HTTP Exporter, for example: "localhost:4318"
		// No need scheme "http://" or "https://" prefix.
		OpenTemeletryHTTPEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	)

	const (
		teamName       = "go_sandbox"
		serviceName    = "poc_otel_sdk"
		serviceVersion = "0.1.0"
		serviceEnv     = "dev"
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otelSdkResources := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
		semconv.DeploymentEnvironmentName(serviceEnv),
		attribute.String("team", teamName),
	)

	tracerCloser := initTracer(ctx, otelSdkResources, OpenTemeletryHTTPEndpoint)
	defer func() {
		if _err := tracerCloser(ctx); _err != nil {
			slog.ErrorContext(ctx, "shutdown otel tracer error", slog.Any("error", _err))
		}
	}()

	meterCloser := initMeter(ctx, otelSdkResources, OpenTemeletryHTTPEndpoint)
	defer func() {
		if _err := meterCloser(ctx); _err != nil {
			slog.ErrorContext(ctx, "shutdown otel meter error", slog.Any("error", _err))
		}
	}()

	handler := &Handler{
		ServiceName: serviceName,
	}

	router := chi.NewRouter()

	router.Use(middleware.Logger)

	// Wrap handlers with OpenTelemetry middleware
	router.Use(otelhttp.NewMiddleware(serviceName,
		otelhttp.WithServerName(serviceName),
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		}),
		otelhttp.WithMeterProvider(otel.GetMeterProvider()),
	))
	router.Use(MetricsMiddleware(serviceName))

	router.Get("/", handler.Homepage)
	router.Post("/login", handler.Login)

	// Expose metrics at /metrics
	router.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    Port,
		Handler: router,
	}

	fmt.Printf("Starting server on %s\n", Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Errorf("failed to start server: %w", err))
	}
}

func initMeter(ctx context.Context, otelResources *resource.Resource, otelHTTPEndpoint string) func(ctx context.Context) error {

	var metricExporter otelSdkMetric.Exporter
	var metricExporterErr error

	otelHTTPEndpoint = strings.TrimSpace(otelHTTPEndpoint)
	metricExporter, metricExporterErr = otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithInsecure(),
		otlpmetrichttp.WithEndpoint(otelHTTPEndpoint),
		otlpmetrichttp.WithURLPath("/v1/metrics"),
		otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
			Enabled:         true,
			InitialInterval: 5 * time.Second,
			MaxInterval:     15 * time.Second,
			MaxElapsedTime:  3 * time.Minute,
		}),
	)

	if metricExporterErr != nil {
		slog.WarnContext(ctx, "failed to create the OpenTelemetry metric HTTP exporter", slog.Any("error", metricExporterErr))
		slog.WarnContext(ctx, "fallback using stdout metric exporter")
		metricExporter, metricExporterErr = stdoutmetric.New()

		// Check once again if metricExporterErr is not nil (probably from stdoutmetric.New())
		if metricExporterErr != nil {
			slog.ErrorContext(ctx, "failed to create the OpenTelemetry metric stdout exporter", slog.Any("error", metricExporterErr))
		}
	} else {
		slog.WarnContext(ctx, "using OpenTelemetry HTTP Exporter", slog.String("endpoint", otelHTTPEndpoint))
	}

	if metricExporter == nil {
		slog.ErrorContext(ctx, "cannot prepare OpenTelemetry Exporter", slog.Any("error", metricExporterErr))
		otel.SetMeterProvider(otelMetricNoop.NewMeterProvider())
		return func(context.Context) error {
			return nil
		}
	}

	// metricExporter must not nil here
	meterProviderOpts := []otelSdkMetric.Option{
		otelSdkMetric.WithResource(otelResources),
		otelSdkMetric.WithReader(
			otelSdkMetric.NewPeriodicReader(metricExporter,
				// Default is 1m. Set to 3s for demonstrative purposes.
				otelSdkMetric.WithInterval(3*time.Second),
				otelSdkMetric.WithTimeout(1*time.Minute),
			),
		),
	}

	// By default, add prometheus exporter to the meter provider.
	// Set up Prometheus exporter
	prometheusExporter, prometheusExporterErr := prometheus.New()
	if prometheusExporterErr != nil {
		slog.ErrorContext(ctx, "failed to create the Prometheus exporter", slog.Any("error", prometheusExporterErr))
	} else {
		meterProviderOpts = append(meterProviderOpts, otelSdkMetric.WithReader(prometheusExporter))
	}

	meterProvider := otelSdkMetric.NewMeterProvider(meterProviderOpts...)
	if meterProvider != nil {
		otel.SetMeterProvider(meterProvider)

		return func(ctx context.Context) error {
			var cumulativeErr error
			if _err := metricExporter.Shutdown(ctx); _err != nil {
				cumulativeErr = fmt.Errorf("failed to stop the metric exporter: %w", _err)
			}

			if prometheusExporter != nil {
				if _err := prometheusExporter.Shutdown(ctx); _err != nil {
					cumulativeErr = fmt.Errorf("failed to stop the prometheus exporter: %w", _err)
				}
			}

			if _err := meterProvider.Shutdown(ctx); _err != nil {
				cumulativeErr = fmt.Errorf("failed to stop the meter provider: %w", _err)
			}

			return cumulativeErr
		}
	}

	otel.SetMeterProvider(otelMetricNoop.NewMeterProvider())
	return func(context.Context) error {
		return nil
	}
}

func initTracer(ctx context.Context, otelResources *resource.Resource, otelHTTPEndpoint string) func(ctx context.Context) error {
	var tracerExporter otelSdkTrace.SpanExporter = tracetest.NewNoopExporter()
	var tracerErr error

	otelHTTPEndpoint = strings.TrimSpace(otelHTTPEndpoint)
	if otelHTTPEndpoint != "" {
		tracerExporter, tracerErr = otlptrace.New(
			ctx,
			otlptracehttp.NewClient(
				otlptracehttp.WithInsecure(),
				otlptracehttp.WithEndpoint(otelHTTPEndpoint),
				otlptracehttp.WithURLPath("/v1/traces"),
				otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
				otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
					Enabled:         true,
					InitialInterval: 5 * time.Second,
					MaxInterval:     15 * time.Second,
					MaxElapsedTime:  3 * time.Minute,
				}),
			),
		)
	}

	if tracerErr != nil {
		otel.SetTracerProvider(otelTraceNoop.NewTracerProvider())
		slog.ErrorContext(ctx, "cannot prepare OpenTelemetry HTTP Exporter", slog.Any("error", tracerErr))
		return func(context.Context) error {
			return nil
		}
	}

	tracerProvider := otelSdkTrace.NewTracerProvider(
		// use sync operation to make sure every span persisted before CLI done
		otelSdkTrace.WithSyncer(tracerExporter),
		otelSdkTrace.WithResource(otelResources),
		otelSdkTrace.WithSampler(otelSdkTrace.AlwaysSample()),
	)

	// Set as global OpenTelemetry tracer provider.
	if tracerProvider != nil {
		otel.SetTracerProvider(tracerProvider)

		return func(context.Context) error {
			var cumulativeErr error
			if _err := tracerExporter.Shutdown(ctx); _err != nil {
				cumulativeErr = fmt.Errorf("failed to stop the tracer exporter: %w", _err)
			}

			if _err := tracerProvider.Shutdown(ctx); _err != nil {
				cumulativeErr = fmt.Errorf("failed to stop the tracer provider: %w", _err)
			}

			return cumulativeErr
		}
	}

	otel.SetTracerProvider(otelTraceNoop.NewTracerProvider())
	return func(context.Context) error {
		return nil
	}
}

func MetricsMiddleware(svcName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		meterProvider := otel.GetMeterProvider().Meter(instrumentationName)

		var err error
		var requestCount metric.Int64Counter
		var requestLatency metric.Int64Histogram

		// Define metrics
		requestCount, err = meterProvider.Int64Counter(svcName + ".http_server_requests_total")
		if err != nil {
			slog.Error("failed to create http_server_requests_total counter", slog.Any("error", err))
			requestCount = &otelMetricNoop.Int64Counter{}
		}

		requestLatency, err = meterProvider.Int64Histogram(svcName + ".http_server_request_duration_ms")
		if err != nil {
			slog.Error("failed to create http_server_request_duration_ms histogram", slog.Any("error", err))
			requestLatency = &otelMetricNoop.Int64Histogram{}
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			tags := []attribute.KeyValue{
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			}

			// Increment the request count
			requestCount.Add(r.Context(), 1, metric.WithAttributes(tags...))

			// Process the request
			next.ServeHTTP(w, r)

			// Record metrics
			duration := time.Since(startTime).Milliseconds()
			requestLatency.Record(r.Context(), duration, metric.WithAttributes(tags...))
		})
	}
}

type Handler struct {
	ServiceName string
}

func (*Handler) Homepage(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("Hello World! (from otel-sdk example).\n"))
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	type User struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	defer span.End()

	parentCtx, parentSpan := span.TracerProvider().Tracer(instrumentationName).Start(ctx, "Login Handler [Otel SDK]")
	defer parentSpan.End()

	loginFailureCtr := appmetrics.LoginFailureCounter(ctx, h.ServiceName)
	loginSuccessCtr := appmetrics.LoginSuccessCounter(ctx, h.ServiceName)

	var user User
	{
		_, decodeBodySpan := parentSpan.TracerProvider().Tracer(instrumentationName).Start(ctx, "Decode Body [Otel SDK]")

		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			loginFailureCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String(appmetrics.FailureReasonKey, appmetrics.LoginInvalidPayload),
			))

			decodeBodySpan.RecordError(err)
			decodeBodySpan.SetAttributes(attribute.String(appmetrics.FailureReasonKey, appmetrics.LoginInvalidPayload))

			http.Error(w, "Invalid request payload (from otel-sdk example).", http.StatusBadRequest)

			decodeBodySpan.End()
			return
		}

		defer func() {
			if _err := r.Body.Close(); _err != nil {
				parentSpan.RecordError(_err)
				slog.ErrorContext(ctx, "failed to close request body", slog.Any("error", _err))
			}
		}()
		decodeBodySpan.End()
	}

	{
		_, checkCredentialsSpan := parentSpan.TracerProvider().Tracer(instrumentationName).Start(ctx, "Check Credentials [Otel SDK]")
		defer checkCredentialsSpan.End()

		// In-memory user store
		var users = map[string]string{
			"user1": "password1",
			"user2": "password2",
			"user3": "password3",
		}

		// Validate the user credentials
		if password, exists := users[user.Username]; exists && password == user.Password {
			loginSuccessCtr.Add(parentCtx, 1)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Login successful (from otel-sdk example).\n"))
			return
		}
	}

	loginFailureCtr.Add(parentCtx, 1, metric.WithAttributes(
		attribute.String(appmetrics.FailureReasonKey, appmetrics.LoginInvalidCredentials),
	))

	err := fmt.Errorf("invalid credentials")
	parentSpan.RecordError(err)
	parentSpan.SetAttributes(attribute.String(appmetrics.FailureReasonKey, appmetrics.LoginInvalidCredentials))

	http.Error(w, "Invalid username or password (from otel-sdk example).", http.StatusUnauthorized)
}
