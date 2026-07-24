package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Event is the HTTP representation of an event submitted to the service.
//
// Payload remains raw JSON because the processor treats its contents as opaque.
// This avoids converting arbitrary JSON into map[string]any and retaining data
// that the asynchronous processor does not need.
type Event struct {
	User      string          `json:"user"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// ValidationLimits bounds the string fields retained while handling an event.
// Lengths are measured in bytes, matching Go string storage and providing a
// predictable memory bound.
type ValidationLimits struct {
	MaxUserLength int
	MaxTypeLength int
}

// FieldViolation describes one invalid request field.
type FieldViolation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError contains all semantic validation failures found in an event.
// Violations are always ordered by field: user, type, timestamp, then payload.
type ValidationError struct {
	Violations []FieldViolation
}

func (ValidationError) Error() string {
	return "event validation failed"
}

// Validate checks the semantic event contract without mutating or normalizing
// the submitted values.
func (e Event) Validate(limits ValidationLimits) error {
	violations := make([]FieldViolation, 0, 4)

	switch {
	case e.User == "":
		violations = append(violations, FieldViolation{
			Field:   "user",
			Message: "is required",
		})
	case strings.TrimSpace(e.User) == "":
		violations = append(violations, FieldViolation{
			Field:   "user",
			Message: "must not be blank",
		})
	case len(e.User) > limits.MaxUserLength:
		violations = append(violations, FieldViolation{
			Field:   "user",
			Message: fmt.Sprintf("must not exceed %d bytes", limits.MaxUserLength),
		})
	}

	switch {
	case e.Type == "":
		violations = append(violations, FieldViolation{
			Field:   "type",
			Message: "is required",
		})
	case strings.TrimSpace(e.Type) == "":
		violations = append(violations, FieldViolation{
			Field:   "type",
			Message: "must not be blank",
		})
	case len(e.Type) > limits.MaxTypeLength:
		violations = append(violations, FieldViolation{
			Field:   "type",
			Message: fmt.Sprintf("must not exceed %d bytes", limits.MaxTypeLength),
		})
	}

	if e.Timestamp <= 0 {
		violations = append(violations, FieldViolation{
			Field:   "timestamp",
			Message: "must be a positive Unix timestamp",
		})
	}

	payload := bytes.TrimSpace(e.Payload)
	switch {
	case len(payload) == 0:
		violations = append(violations, FieldViolation{
			Field:   "payload",
			Message: "is required",
		})
	case !json.Valid(payload) || payload[0] != '{':
		violations = append(violations, FieldViolation{
			Field:   "payload",
			Message: "must be a valid JSON object",
		})
	}

	if len(violations) == 0 {
		return nil
	}

	return ValidationError{Violations: violations}
}
