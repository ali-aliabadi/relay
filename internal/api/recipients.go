package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

type recipientRequest struct {
	Username          string   `json:"username"`
	DisplayName       string   `json:"display_name"`
	Timezone          string   `json:"timezone"`
	ChannelPreference []string `json:"channel_preference"`
}

type recipientResponse struct {
	Username          string   `json:"username"`
	Aliases           []string `json:"aliases"`
	DisplayName       string   `json:"display_name"`
	Timezone          string   `json:"timezone"`
	ChannelPreference []string `json:"channel_preference"`
	LinkedChannels    []string `json:"linked_channels"`
	CreatedAt         string   `json:"created_at"`
}

type recipientHandlers struct {
	logger *slog.Logger
	svc    *core.Recipients
}

func (h recipientHandlers) response(r *http.Request, rcp store.Recipient) (recipientResponse, error) {
	linked, err := h.svc.Channels(r.Context(), rcp.ID)
	if err != nil {
		return recipientResponse{}, err
	}
	aliases, err := h.svc.Aliases(r.Context(), rcp.ID)
	if err != nil {
		return recipientResponse{}, err
	}
	return recipientResponse{
		Username: rcp.Username, Aliases: aliases, DisplayName: rcp.DisplayName, Timezone: rcp.Timezone,
		ChannelPreference: rcp.ChannelPreference, LinkedChannels: linked,
		CreatedAt: rcp.CreatedAt.Format(time.RFC3339),
	}, nil
}

func (h recipientHandlers) list(w http.ResponseWriter, r *http.Request) {
	rs, err := h.svc.List(r.Context())
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	out := make([]recipientResponse, 0, len(rs))
	for _, rcp := range rs {
		resp, err := h.response(r, rcp)
		if err != nil {
			writeServiceError(w, r, h.logger, err)
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipients": out})
}

func (h recipientHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req recipientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rcp, err := h.svc.Add(r.Context(), store.Recipient{
		Username: req.Username, DisplayName: req.DisplayName, Timezone: req.Timezone,
		ChannelPreference: req.ChannelPreference,
	})
	h.respond(w, r, http.StatusCreated, rcp, err)
}

func (h recipientHandlers) get(w http.ResponseWriter, r *http.Request) {
	rcp, err := h.svc.Get(r.Context(), r.PathValue("username"))
	h.respond(w, r, http.StatusOK, rcp, err)
}

// update replaces display_name, timezone and channel_preference. The
// username is the path's and can't be changed.
func (h recipientHandlers) update(w http.ResponseWriter, r *http.Request) {
	var req recipientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	username := r.PathValue("username")
	if req.Username != "" && req.Username != username {
		writeError(w, http.StatusUnprocessableEntity, "invalid_request", "username can't be changed")
		return
	}
	err := h.svc.Update(r.Context(), store.Recipient{
		Username: username, DisplayName: req.DisplayName, Timezone: req.Timezone,
		ChannelPreference: req.ChannelPreference,
	})
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	rcp, err := h.svc.Get(r.Context(), username)
	h.respond(w, r, http.StatusOK, rcp, err)
}

func (h recipientHandlers) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Remove(r.Context(), r.PathValue("username")); err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h recipientHandlers) respond(w http.ResponseWriter, r *http.Request, status int, rcp store.Recipient, err error) {
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	resp, err := h.response(r, rcp)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, status, resp)
}
