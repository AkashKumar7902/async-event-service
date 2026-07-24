package api

import (
	"encoding/json"
	"net/http"

	"github.com/akashkumar7902/async-event-service/internal/model"
)

type acceptedResponse struct {
	Status string `json:"status"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Fields  []model.FieldViolation `json:"fields,omitempty"`
}

func writeError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
	fields []model.FieldViolation,
) {
	writeJSON(w, status, errorEnvelope{
		Error: errorBody{
			Code:    code,
			Message: message,
			Fields:  fields,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Values passed to this helper are limited to known response structs and a
	// map[string]uint64 snapshot, so encoding cannot fail because of a value.
	// A client disconnect is handled by net/http and does not undo admission.
	_ = json.NewEncoder(w).Encode(value)
}
