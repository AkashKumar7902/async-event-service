package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/akashkumar7902/async-event-service/internal/model"
	"github.com/akashkumar7902/async-event-service/internal/processor"
	"github.com/akashkumar7902/async-event-service/internal/stats"
)

type Handler struct {
	processor    *processor.Processor
	stats        *stats.Store
	maxBodyBytes int64
	validation   model.ValidationLimits
	logger       *slog.Logger
}

type Options struct {
	Processor    *processor.Processor
	Stats        *stats.Store
	MaxBodyBytes int64
	Validation   model.ValidationLimits
	Logger       *slog.Logger
}

func NewHandler(options Options) (*Handler, error) {
	switch {
	case options.Processor == nil:
		return nil, errors.New("event processor is required")
	case options.Stats == nil:
		return nil, errors.New("statistics store is required")
	case options.Logger == nil:
		return nil, errors.New("logger is required")
	case options.MaxBodyBytes <= 0:
		return nil, errors.New("maximum request body size must be positive")
	case options.Validation.MaxUserLength <= 0:
		return nil, errors.New("maximum user length must be positive")
	case options.Validation.MaxTypeLength <= 0:
		return nil, errors.New("maximum event type length must be positive")
	}

	return &Handler{
		processor:    options.Processor,
		stats:        options.Stats,
		maxBodyBytes: options.MaxBodyBytes,
		validation:   options.Validation,
		logger:       options.Logger,
	}, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", h.postEvent)
	mux.HandleFunc("GET /stats", h.getStats)
	mux.HandleFunc("/events", methodNotAllowed(http.MethodPost))
	mux.HandleFunc("/stats", methodNotAllowed(http.MethodGet+", "+http.MethodHead))
	mux.HandleFunc("/", notFound)
	return mux
}

func (h *Handler) postEvent(w http.ResponseWriter, r *http.Request) {
	event, err := decodeEvent(w, r, h.maxBodyBytes)
	if err != nil {
		switch {
		case errors.Is(err, errUnsupportedMediaType):
			writeError(
				w,
				http.StatusUnsupportedMediaType,
				"unsupported_media_type",
				"Content-Type must be application/json",
				nil,
			)
		case errors.Is(err, errRequestTooLarge):
			writeError(
				w,
				http.StatusRequestEntityTooLarge,
				"request_too_large",
				"request body exceeds the maximum allowed size",
				nil,
			)
		default:
			writeError(
				w,
				http.StatusBadRequest,
				"invalid_json",
				"request body must contain one valid JSON object",
				nil,
			)
		}
		return
	}

	if err := event.Validate(h.validation); err != nil {
		if violations, ok := validationViolations(err); ok {
			writeError(
				w,
				http.StatusBadRequest,
				"validation_failed",
				"request validation failed",
				violations,
			)
			return
		}

		h.logInternalFailure(r, "event validation failed unexpectedly")
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
			nil,
		)
		return
	}

	switch err := h.processor.TrySubmit(event.Type); {
	case err == nil:
		writeJSON(w, http.StatusAccepted, acceptedResponse{Status: "accepted"})
	case errors.Is(err, processor.ErrQueueFull):
		w.Header().Set("Retry-After", "1")
		writeError(
			w,
			http.StatusServiceUnavailable,
			"queue_full",
			"event queue is temporarily full",
			nil,
		)
	case errors.Is(err, processor.ErrShuttingDown):
		writeError(
			w,
			http.StatusServiceUnavailable,
			"shutting_down",
			"service is shutting down",
			nil,
		)
	default:
		// Do not log the raw error, request body, user, or event type. A stable
		// message and bounded request metadata are enough to correlate failures.
		h.logInternalFailure(r, "event submission failed unexpectedly")
		writeError(
			w,
			http.StatusInternalServerError,
			"internal_error",
			"internal server error",
			nil,
		)
	}
}

func (h *Handler) getStats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	snapshot := h.stats.Snapshot()
	if snapshot == nil {
		snapshot = make(map[string]uint64)
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) logInternalFailure(r *http.Request, message string) {
	h.logger.Error(
		message,
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(
			w,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"HTTP method is not allowed for this resource",
			nil,
		)
	}
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(
		w,
		http.StatusNotFound,
		"not_found",
		"requested resource was not found",
		nil,
	)
}

func validationViolations(err error) ([]model.FieldViolation, bool) {
	var valueError model.ValidationError
	if errors.As(err, &valueError) {
		return valueError.Violations, true
	}

	var pointerError *model.ValidationError
	if errors.As(err, &pointerError) {
		return pointerError.Violations, true
	}

	return nil, false
}
