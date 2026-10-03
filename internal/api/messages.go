package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

const defaultListLimit = 50

type messageHandlers struct {
	logger *slog.Logger
	svc    *core.Messages
}

type messageSummary struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Urgency   string `json:"urgency"`
	Source    string `json:"source,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	CreatedAt string `json:"created_at"`
	Redacted  bool   `json:"redacted"`
}

type deliveryResponse struct {
	ID            string `json:"id"`
	RecipientID   string `json:"recipient_id"`
	Channel       string `json:"channel"`
	Status        string `json:"status"`
	Attempts      int64  `json:"attempts"`
	NextAttemptAt string `json:"next_attempt_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	UpdatedAt     string `json:"updated_at"`
}

func summary(m store.Message) messageSummary {
	return messageSummary{
		ID: m.ID, Status: m.Status, Urgency: m.Urgency, Source: m.Source, RequestID: m.RequestID,
		CreatedAt: m.CreatedAt.Format(time.RFC3339Nano), Redacted: m.RedactedAt != nil,
	}
}

func (h messageHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req message.Request
	if !decodeJSON(w, r, &req) {
		return
	}
	msg, created, err := h.svc.Create(r.Context(), clientFrom(r.Context()).ID, obs.RequestID(r.Context()), req)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]string{"id": msg.ID, "status": msg.Status})
}

func (h messageHandlers) get(w http.ResponseWriter, r *http.Request) {
	msg, ds, err := h.svc.Get(r.Context(), clientFrom(r.Context()).ID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	out := make([]deliveryResponse, 0, len(ds))
	for _, d := range ds {
		dr := deliveryResponse{
			ID: d.ID, RecipientID: d.RecipientID, Channel: d.Channel, Status: d.Status, Attempts: d.Attempts,
			LastError: d.LastError, UpdatedAt: d.UpdatedAt.Format(time.RFC3339Nano),
		}
		if d.Status == store.StatusQueued {
			dr.NextAttemptAt = d.NextAttemptAt.Format(time.RFC3339Nano)
		}
		out = append(out, dr)
	}
	writeJSON(w, http.StatusOK, struct {
		messageSummary
		Deliveries []deliveryResponse `json:"deliveries"`
	}{summary(msg), out})
}

func (h messageHandlers) list(w http.ResponseWriter, r *http.Request) {
	f, ok := parseListFilter(w, r)
	if !ok {
		return
	}
	ms, err := h.svc.List(r.Context(), f)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	out := make([]messageSummary, 0, len(ms))
	for _, m := range ms {
		out = append(out, summary(m))
	}
	resp := map[string]any{"messages": out}
	if len(ms) == f.Limit {
		resp["next_cursor"] = ms[len(ms)-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}

var listStatuses = map[string]bool{
	store.StatusQueued: true, store.StatusSending: true, store.StatusDelivered: true,
	store.StatusPartiallyDelivered: true, store.StatusFailed: true,
}

func parseListFilter(w http.ResponseWriter, r *http.Request) (store.ListFilter, bool) {
	q := r.URL.Query()
	f := store.ListFilter{ClientID: clientFrom(r.Context()).ID, Limit: defaultListLimit, BeforeID: q.Get("cursor")}
	if s := q.Get("status"); s != "" {
		if !listStatuses[s] {
			writeError(w, http.StatusUnprocessableEntity, "invalid_request", "status: unknown status")
			return f, false
		}
		f.Status = s
	}
	if s := q.Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_request", "since: must be an RFC 3339 time")
			return f, false
		}
		f.Since = t
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > store.MaxListLimit {
			writeError(w, http.StatusUnprocessableEntity, "invalid_request", "limit: must be 1-100")
			return f, false
		}
		f.Limit = n
	}
	return f, true
}

func (h messageHandlers) preview(w http.ResponseWriter, r *http.Request) {
	var req message.Request
	if !decodeJSON(w, r, &req) {
		return
	}
	previews, err := h.svc.Preview(req)
	if err != nil {
		writeServiceError(w, r, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": previews})
}

func (h messageHandlers) channels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"channels": h.svc.Channels(r.Context())})
}
