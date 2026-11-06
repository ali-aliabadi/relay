package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

func (h messageHandlers) editText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := h.svc.EditText(ctx, clientFrom(ctx).ID, r.PathValue("id"), req.Text)
	var ve *message.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "status": "updated"})
	case errors.Is(err, core.ErrNotEditable):
		writeError(w, http.StatusConflict, "not_editable", "message is not an editable delivered text message")
	case errors.Is(err, store.ErrNotFound), errors.As(err, &ve):
		writeServiceError(w, r, h.logger, err)
	default:
		// Never log a provider error here: it may contain the edited text.
		writeError(w, http.StatusBadGateway, "update_failed", "message update could not be delivered")
	}
}
