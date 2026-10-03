package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// decodeJSON strictly decodes one JSON object from the body into v. On
// failure it writes the error response and returns false. Error messages
// describe the problem without quoting the body.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data")
	}
	if err == nil {
		return true
	}
	var tooBig *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body is too large")
	case errors.As(err, &typeErr):
		writeError(w, http.StatusBadRequest, "invalid_json", "wrong type for field "+typeErr.Field)
	case isUnknownField(err):
		writeError(w, http.StatusBadRequest, "invalid_json", "request has an unknown field")
	default:
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be one JSON object")
	}
	return false
}

func isUnknownField(err error) bool {
	// encoding/json has no typed error for this; the message names the field,
	// which is caller-controlled, so it is matched here and never echoed.
	return strings.HasPrefix(err.Error(), "json: unknown field ")
}

// writeServiceError maps core and store errors to API responses. Anything
// unexpected is logged by ID-only context and returned as a bare 500.
func writeServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var ve *message.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": map[string]any{
			"code": "invalid_request", "message": ve.Error(), "problems": ve.Problems,
		}})
	case errors.Is(err, core.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "already exists")
	default:
		logger.ErrorContext(r.Context(), "request failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
