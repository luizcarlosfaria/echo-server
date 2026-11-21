// Package dashboard atende as rotas reservadas sob /_echo, que não são ecoadas nem entram
// no histórico: o painel, o script dele, a API do histórico e o WebSocket.
package dashboard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/luizcarlosfaria/echo-server/internal/config"
	"github.com/luizcarlosfaria/echo-server/internal/forwarded"
	"github.com/luizcarlosfaria/echo-server/internal/model"
	"github.com/luizcarlosfaria/echo-server/internal/store"
	"github.com/luizcarlosfaria/echo-server/internal/web"
)

// Prefix é o prefixo das rotas reservadas.
const Prefix = "/_echo"

// IsReserved indica se o path está sob /_echo (comparação sem diferenciar maiúsculas,
// por segmento: /_echo e /_echo/x são reservados, /_echox não).
func IsReserved(path string) bool {
	if len(path) < len(Prefix) || !strings.EqualFold(path[:len(Prefix)], Prefix) {
		return false
	}
	return len(path) == len(Prefix) || path[len(Prefix)] == '/'
}

// Handler atende /_echo/*.
type Handler struct {
	options  config.Options
	store    *store.Store
	stopping context.Context
}

// New cria o handler. stopping é cancelado no shutdown, encerrando os WebSockets abertos.
func New(options config.Options, st *store.Store, stopping context.Context) *Handler {
	return &Handler{options: options, store: st, stopping: stopping}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.ToLower(r.URL.Path[len(Prefix):])

	switch {
	case rest == "" || rest == "/":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		h.servePage(w, r)

	case rest == "/dashboard.js":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		web.ServeDashboardScript(w, r)

	case rest == "/api/requests":
		switch r.Method {
		case http.MethodGet:
			h.serveList(w, r)
		case http.MethodDelete:
			h.store.Clear()
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, "GET, DELETE")
		}

	case strings.HasPrefix(rest, "/api/requests/"):
		id, err := strconv.ParseInt(rest[len("/api/requests/"):], 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		if request := h.store.Get(id); request != nil {
			writeJSON(w, http.StatusOK, request)
			return
		}
		w.WriteHeader(http.StatusNotFound)

	case rest == "/ws":
		h.serveWebSocket(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request) {
	info := forwarded.FromRequest(r)
	base := info.PathBase + Prefix + "/"
	err := web.RenderDashboard(w, web.DashboardPage{
		Title:       h.options.AppName + " · Live requests",
		AppName:     h.options.AppName,
		HeaderColor: h.options.HeaderColor,
		Base:        base,
		Capacity:    h.store.Capacity(),
		Origin:      info.Scheme + "://" + info.Host,
		ScriptURL:   base + "dashboard.js?v=" + web.DashboardScriptVersion(),
	})
	if err != nil {
		slog.Error("failed to render the dashboard", "error", err)
	}
}

type listResponse struct {
	Capacity      int             `json:"capacity"`
	Count         int             `json:"count"`
	TotalReceived int64           `json:"totalReceived"`
	Items         []model.Summary `json:"items"`
}

func (h *Handler) serveList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, http.StatusOK, listResponse{
		Capacity:      h.store.Capacity(),
		Count:         h.store.Count(),
		TotalReceived: h.store.TotalReceived(),
		Items:         summaries(h.store.Snapshot(limit)),
	})
}

func summaries(requests []*model.Request) []model.Summary {
	result := make([]model.Summary, 0, len(requests))
	for _, request := range requests {
		result = append(result, request.Summary())
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		slog.Error("failed to serialize the response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	w.WriteHeader(http.StatusMethodNotAllowed)
}
