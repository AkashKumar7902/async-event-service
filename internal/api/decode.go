package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/akashkumar7902/async-event-service/internal/model"
)

var (
	errInvalidJSON          = errors.New("request body must contain one valid JSON value")
	errRequestTooLarge      = errors.New("request body exceeds the configured limit")
	errUnsupportedMediaType = errors.New("content type must be application/json")
)

func decodeEvent(
	w http.ResponseWriter,
	r *http.Request,
	maxBodyBytes int64,
) (model.Event, error) {
	if err := requireJSONContentType(r.Header.Get("Content-Type")); err != nil {
		return model.Event{}, err
	}
	if r.ContentLength > maxBodyBytes {
		return model.Event{}, errRequestTooLarge
	}

	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	var event model.Event
	if err := decoder.Decode(&event); err != nil {
		return model.Event{}, classifyDecodeError(err)
	}

	// A second decode both rejects concatenated JSON values and makes
	// MaxBytesReader observe any trailing bytes beyond the configured limit.
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	switch {
	case errors.Is(err, io.EOF):
		return event, nil
	case err != nil:
		return model.Event{}, classifyDecodeError(err)
	default:
		return model.Event{}, errInvalidJSON
	}
}

func requireJSONContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	return nil
}

func classifyDecodeError(err error) error {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return errRequestTooLarge
	}
	return errInvalidJSON
}
