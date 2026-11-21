package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/luizcarlosfaria/echo-server/internal/model"
	"github.com/luizcarlosfaria/echo-server/internal/store"
)

const (
	// perClientQueue: um cliente lento perde os eventos mais antigos, mas nunca atrasa o echo
	// nem os outros clientes.
	perClientQueue = 512
	keepAlive      = 30 * time.Second
	writeTimeout   = 10 * time.Second
)

type snapshotMessage struct {
	Type          string          `json:"type"`
	Capacity      int             `json:"capacity"`
	TotalReceived int64           `json:"totalReceived"`
	Items         []model.Summary `json:"items"`
}

type requestMessage struct {
	Type          string        `json:"type"`
	TotalReceived int64         `json:"totalReceived"`
	Item          model.Summary `json:"item"`
}

type clearedMessage struct {
	Type          string `json:"type"`
	TotalReceived int64  `json:"totalReceived"`
}

// serveWebSocket: ao conectar envia um snapshot (resumos) e depois um evento por request
// recebido ou limpeza do histórico. O cliente não envia nada útil.
func (h *Handler) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "This endpoint only accepts WebSocket connections.")
		return
	}

	// Sem checagem de Origin: atrás de proxies o Host costuma ser reescrito.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Debug("dashboard WebSocket handshake failed", "error", err)
		return
	}
	defer conn.CloseNow()

	snapshot, totalReceived, events, unsubscribe := h.store.Subscribe(perClientQueue)
	defer unsubscribe()

	// CloseRead lê (e descarta) o que o cliente mandar, responde pings e cancela ctx quando
	// a conexão fecha. Derivado de stopping: o shutdown também encerra o loop.
	ctx := conn.CloseRead(h.stopping)

	if err := send(ctx, conn, snapshotMessage{
		Type: "snapshot", Capacity: h.store.Capacity(), TotalReceived: totalReceived, Items: summaries(snapshot),
	}); err != nil {
		return
	}

	ticker := time.NewTicker(keepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if h.stopping.Err() != nil {
				_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
			}
			return

		case event := <-events:
			var message any
			if event.Type == store.EventRequest {
				message = requestMessage{Type: "request", TotalReceived: event.TotalReceived, Item: event.Request.Summary()}
			} else {
				message = clearedMessage{Type: "cleared", TotalReceived: event.TotalReceived}
			}
			if err := send(ctx, conn, message); err != nil {
				slog.Debug("dashboard WebSocket closed", "error", err)
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func send(ctx context.Context, conn *websocket.Conn, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, payload)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") && headerHasToken(r.Header, "Upgrade", "websocket")
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, line := range header.Values(name) {
		for _, item := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(item), token) {
				return true
			}
		}
	}
	return false
}
