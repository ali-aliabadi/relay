package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

func (h messageHandlers) deleteText(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := h.svc.DeleteText(ctx, clientFrom(ctx).ID, r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, core.ErrNotDeletable):
		writeError(w, http.StatusConflict, "not_deletable", "message is not a deletable delivered text message")
	case errors.Is(err, store.ErrNotFound):
		writeServiceError(w, r, h.logger, err)
	default:
		// Provider errors must never expose content or contact details.
		writeError(w, http.StatusBadGateway, "delete_failed", "message deletion could not be delivered")
	}
}
