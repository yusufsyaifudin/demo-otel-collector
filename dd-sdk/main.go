package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	// Go-Chi router
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	// Datadog client
	"github.com/DataDog/datadog-go/v5/statsd"
	chitrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/go-chi/chi.v5"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func main() {
	var (
		Port = os.Getenv("PORT")

		// DatadogAgentHost MUST open port 8125 (statsd) and 8126 (trace-agent) on Datadog Agent
		// For example, 127.0.0.1
		DatadogAgentHost = os.Getenv("DATADOG_AGENT_HOST")
	)

	const (
		teamName       = "go_sandbox"
		serviceName    = "poc_dd_sdk_statsd"
		serviceVersion = "0.1.0"
		serviceEnv     = "dev"
	)

	// Start the tracer
	tracer.Start(
		tracer.WithAgentAddr(fmt.Sprintf("%s:8126", DatadogAgentHost)),
		tracer.WithGlobalTag("team", teamName), // Adding a global tag
		tracer.WithService(serviceName),
		tracer.WithUniversalVersion(serviceVersion),
		tracer.WithEnv(serviceEnv),
		tracer.WithLogStartup(false),
		tracer.WithDebugMode(false),
	)
	defer tracer.Stop()

	var err error
	statsdClient, err := statsd.New(
		fmt.Sprintf("%s:8125", DatadogAgentHost),
		statsd.WithNamespace(serviceName),
		statsd.WithTags([]string{
			fmt.Sprintf("service:%s", serviceName),
			fmt.Sprintf("version:%s", serviceVersion),
			fmt.Sprintf("team:%s", teamName),
		}),
	) // Default Datadog agent statsd port
	if err != nil {
		panic(err)
	}

	handler := &Handler{
		StatsdClient: statsdClient,
	}

	// Create a chi Router
	router := chi.NewRouter()

	router.Use(middleware.Logger)

	// Use the tracer middleware with the default service name "chi.router".
	router.Use(chitrace.Middleware(
		chitrace.WithServiceName(serviceName),
	))

	// Set up some endpoints.
	router.Get("/", handler.Homepage)
	router.Post("/login", handler.Login)

	// Start the HTTP server
	http.ListenAndServe(Port, router)
}

type Handler struct {
	// Datadog StatsD client
	StatsdClient *statsd.Client
}

func (*Handler) Homepage(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("Hello World! (from dd-sdk example).\n"))
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	// User represents the login credentials structure
	type User struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	ctx := r.Context()
	var err error

	// Create a span which will be the child of the span in the Context ctx, if there is a span in the context.
	parentSpan, _ := tracer.StartSpanFromContext(ctx, "Login Handler [DD-SDK]",
		tracer.ResourceName("login-handler"),
	)
	defer parentSpan.Finish(
		tracer.WithError(err),
	)

	var user User
	{
		// Creating a children to this new span
		decodeBodySpan := tracer.StartSpan("Decode Body [DD-SDK]",
			tracer.ResourceName("decode-body"),
			tracer.ChildOf(parentSpan.Context()),
		)

		err = json.NewDecoder(r.Body).Decode(&user)
		if err != nil {
			if _err := h.StatsdClient.Incr("login.failure", []string{"reason:invalid_payload"}, 1); _err != nil {
				slog.ErrorContext(ctx, "failed to increment login failure counter", slog.Any("error", _err))
			}

			http.Error(w, "Invalid request payload (from dd-sdk example).", http.StatusBadRequest)
			decodeBodySpan.Finish()
			return
		}

		defer func() {
			if _err := r.Body.Close(); _err != nil {
				slog.ErrorContext(ctx, "failed to close request body", slog.Any("error", _err))
			}
		}()
		decodeBodySpan.Finish()
	}

	{
		checkCredentialsSpan := tracer.StartSpan("Check Credentials [DD-SDK]",
			tracer.ResourceName("check-credentials"),
			tracer.ChildOf(parentSpan.Context()),
		)
		defer checkCredentialsSpan.Finish()

		// In-memory user store
		var users = map[string]string{
			"user1": "password1",
			"user2": "password2",
			"user3": "password3",
		}

		// Validate the user credentials
		if password, exists := users[user.Username]; exists && password == user.Password {
			if _err := h.StatsdClient.Incr("login.success", nil, 1); _err != nil {
				slog.ErrorContext(ctx, "failed to increment login success counter", slog.Any("error", _err))
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Login successful (from dd-sdk example).\n"))
			return
		}
	}

	if _err := h.StatsdClient.Incr("login.failure", []string{"reason:invalid_credentials"}, 1); _err != nil {
		slog.ErrorContext(ctx, "failed to increment login failure counter", slog.Any("error", _err))
	}

	err = fmt.Errorf("invalid credentials") // for defer function error tracer.RecordError(err)
	http.Error(w, "Invalid username or password (from dd-sdk example).", http.StatusUnauthorized)
}
