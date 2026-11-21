package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// ECHO_DASHBOARD_PATH move o painel, a API e o WebSocket; o prefixo antigo volta a ser ecoado.
func TestCustomDashboardPath(t *testing.T) {
	a := start(t, map[string]string{"ECHO_DASHBOARD_PATH": "ops/echo/"})

	// Painel, script, API e WebSocket no prefixo novo (sem diferenciar maiúsculas).
	response, body := a.send(t, request{path: "/ops/echo"})
	page := string(body)
	if response.StatusCode != http.StatusOK || !strings.Contains(page, `data-base="/ops/echo/"`) || !strings.Contains(page, `src="/ops/echo/dashboard.js?v=`) {
		t.Fatalf("dashboard: status %d\n%s", response.StatusCode, page)
	}
	for _, path := range []string{"/ops/echo/", "/OPS/Echo/dashboard.js", "/ops/echo/api/requests"} {
		if response, _ := a.send(t, request{path: path}); response.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, response.StatusCode)
		}
	}
	if response, _ := a.send(t, request{path: "/ops/echo/ws"}); response.StatusCode != http.StatusBadRequest {
		t.Errorf("plain HTTP on the WebSocket: status %d", response.StatusCode)
	}
	if total := a.getJSON(t, "/ops/echo/api/requests").num("totalReceived"); total != 0 {
		t.Fatalf("dashboard routes must not be captured, totalReceived = %d", total)
	}

	// O prefixo padrão deixa de ser reservado e passa a ser ecoado; /ops/echox também.
	for _, path := range []string{"/_echo", "/_echo/api/requests", "/ops/echox"} {
		if echo := a.echo(t, request{path: path}); echo.str("Path") != path {
			t.Errorf("%s must be echoed, got %v", path, echo["Path"])
		}
	}

	// O link "View live requests" da página HTML do echo aponta para o prefixo novo,
	// respeitando o X-Forwarded-Prefix.
	_, body = a.send(t, request{path: "/x", header: http.Header{"Accept": {"text/html"}, "X-Forwarded-Prefix": {"/app"}}})
	if !strings.Contains(string(body), `href="/app/ops/echo"`) {
		t.Fatal("the echo page must link to the configured dashboard path")
	}

	// WebSocket no prefixo novo recebe o snapshot com o que já foi ecoado.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(a.server.URL, "http")+"/ops/echo/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if snapshot := receive(t, conn); snapshot.str("type") != "snapshot" || len(snapshot.arr("items")) != 4 {
		t.Fatalf("snapshot = %v", snapshot)
	}
}
