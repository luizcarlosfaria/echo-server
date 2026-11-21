package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func ids(list obj) []int64 {
	var result []int64
	for _, item := range list.arr("items") {
		result = append(result, obj(item.(map[string]any)).num("id"))
	}
	return result
}

func TestEachRequestIsStoredAndRetrievableByID(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/pedidos?x=1", contentType: "application/json", body: strings.NewReader(`{"valor":10}`)})

	stored := a.getJSON(t, fmt.Sprintf("/_echo/api/requests/%d", echo.num("RequestId")))
	if stored.str("method") != "POST" || stored.str("path") != "/pedidos" || stored.str("queryString") != "?x=1" ||
		stored.obj("body").obj("json").num("valor") != 10 {
		t.Fatalf("stored = %v", stored)
	}
}

func TestResponseCarriesRequestIDHeader(t *testing.T) {
	a := start(t, nil)
	response, _ := a.send(t, request{path: "/com-header"})
	if response.Header.Get("X-Echo-Request-Id") != "1" {
		t.Fatalf("X-Echo-Request-Id = %q", response.Header.Get("X-Echo-Request-Id"))
	}
}

func TestDefaultCapacityIs1000(t *testing.T) {
	a := start(t, nil)
	if a.getJSON(t, "/_echo/api/requests").num("capacity") != 1000 {
		t.Fatal("default capacity must be 1000")
	}
}

func TestOldestRequestsArePurged(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_REQUESTS": "5"})
	for i := 1; i <= 8; i++ {
		a.echo(t, request{path: fmt.Sprintf("/r%d", i)})
	}

	list := a.getJSON(t, "/_echo/api/requests")
	if list.num("capacity") != 5 || list.num("count") != 5 || list.num("totalReceived") != 8 || !slices.Equal(ids(list), []int64{8, 7, 6, 5, 4}) {
		t.Fatalf("list = %v", list)
	}
	if response, _ := a.send(t, request{path: "/_echo/api/requests/3"}); response.StatusCode != http.StatusNotFound {
		t.Fatalf("purged request: status %d", response.StatusCode)
	}
}

func TestListHonorsLimit(t *testing.T) {
	a := start(t, nil)
	for range 4 {
		a.echo(t, request{path: "/x"})
	}
	if got := ids(a.getJSON(t, "/_echo/api/requests?limit=2")); !slices.Equal(got, []int64{4, 3}) {
		t.Fatalf("ids = %v", got)
	}
}

func TestDashboardRoutesAreNotCaptured(t *testing.T) {
	a := start(t, nil)
	for _, path := range []string{"/_echo", "/_echo/", "/_echo/dashboard.js", "/_ECHO/api/requests", "/favicon.ico"} {
		if response, _ := a.send(t, request{path: path}); response.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, response.StatusCode)
		}
	}
	for _, req := range []request{{path: "/_echo/nao-existe"}, {path: "/_echo/api/requests/abc"}, {method: "POST", path: "/_echo"}} {
		if response, _ := a.send(t, req); response.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status %d", req.method, req.path, response.StatusCode)
		}
	}
	if total := a.getJSON(t, "/_echo/api/requests").num("totalReceived"); total != 0 {
		t.Fatalf("totalReceived = %d", total)
	}
	// /_echox não é reservado.
	if echo := a.echo(t, request{path: "/_echox"}); echo.str("Path") != "/_echox" {
		t.Fatal("/_echox must be echoed")
	}
}

func TestDashboardPageIsServed(t *testing.T) {
	a := start(t, nil)
	response, body := a.send(t, request{path: "/_echo/", header: http.Header{"X-Forwarded-Prefix": {"/app"}}})
	html := string(body)
	if response.StatusCode != http.StatusOK || !strings.Contains(html, "echo-dashboard") || !strings.Contains(html, `src="/app/_echo/dashboard.js?v=`) ||
		!strings.Contains(html, `data-base="/app/_echo/"`) || !strings.Contains(html, "Echo Tests") {
		t.Fatalf("status %d:\n%s", response.StatusCode, html)
	}

	script, js := a.send(t, request{path: "/_echo/dashboard.js"})
	if !strings.HasPrefix(script.Header.Get("Content-Type"), "text/javascript") || !strings.Contains(string(js), "echo-dashboard") {
		t.Fatalf("dashboard.js: %s", script.Header.Get("Content-Type"))
	}
}

func TestClearRemovesEverything(t *testing.T) {
	a := start(t, nil)
	a.echo(t, request{path: "/a"})

	response, _ := a.send(t, request{method: http.MethodDelete, path: "/_echo/api/requests"})
	if response.StatusCode != http.StatusNoContent || a.getJSON(t, "/_echo/api/requests").num("count") != 0 {
		t.Fatalf("status %d", response.StatusCode)
	}
}

func TestLargeBodiesAreTruncatedButSizeIsPreserved(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_BODY_BYTES": "100"})
	echo := a.echo(t, request{method: "POST", path: "/grande", contentType: "text/plain; charset=utf-8", body: strings.NewReader(strings.Repeat("x", 1000))})

	body := echo.body()
	if !body.bool("Truncated") || body.num("Size") != 1000 || len(body.str("Text")) != 100 {
		t.Fatalf("body = %v", body)
	}
}

func TestTruncatedJSONIsNotParsed(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_BODY_BYTES": "10"})
	echo := a.echo(t, request{method: "POST", path: "/json-grande", contentType: "application/json", body: strings.NewReader(`{"campo":"` + strings.Repeat("y", 100) + `"}`)})
	if echo.has("Payload") || !strings.Contains(echo.body().str("JsonError"), "truncated") {
		t.Fatalf("echo = %v", echo)
	}
}

func TestConcurrentRequestsAreAllStoredWithUniqueIDs(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_REQUESTS": "100"})
	var mu sync.Mutex
	var got []int64
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			id := a.echo(t, request{path: fmt.Sprintf("/c%d", i)}).num("RequestId")
			mu.Lock()
			got = append(got, id)
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.Sort(got)
	if len(slices.Compact(got)) != 50 || a.getJSON(t, "/_echo/api/requests").num("count") != 50 {
		t.Fatalf("ids = %v", got)
	}
}

// ---------------------------------------------------------------- WebSocket

func dial(t *testing.T, a *testApp) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(a.server.URL, "http")+"/_echo/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func receive(t *testing.T, conn *websocket.Conn) obj {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWebSocketSendsSnapshotThenLiveRequests(t *testing.T) {
	a := start(t, nil)
	a.echo(t, request{path: "/antes"})

	conn := dial(t, a)
	snapshot := receive(t, conn)
	if snapshot.str("type") != "snapshot" || snapshot.num("capacity") != 1000 || obj(snapshot.arr("items")[0].(map[string]any)).str("path") != "/antes" {
		t.Fatalf("snapshot = %v", snapshot)
	}

	a.echo(t, request{method: "POST", path: "/depois", contentType: "application/json", body: strings.NewReader(`{"a":1}`)})
	live := receive(t, conn)
	item := live.obj("item")
	if live.str("type") != "request" || item.str("path") != "/depois" || item.str("method") != "POST" || item.str("bodyKind") != "json" || live.num("totalReceived") != 2 {
		t.Fatalf("live = %v", live)
	}

	a.send(t, request{method: http.MethodDelete, path: "/_echo/api/requests"})
	if receive(t, conn).str("type") != "cleared" {
		t.Fatal("expected a cleared event")
	}
	_ = conn.Close(websocket.StatusNormalClosure, "fim")
}

func TestWebSocketEndpointRejectsPlainHTTP(t *testing.T) {
	a := start(t, nil)
	if response, _ := a.send(t, request{path: "/_echo/ws"}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", response.StatusCode)
	}
}

func TestMultipleWebSocketClientsReceiveTheSameEvents(t *testing.T) {
	a := start(t, nil)
	first, second := dial(t, a), dial(t, a)
	receive(t, first)
	receive(t, second)

	a.echo(t, request{path: "/broadcast"})

	for _, conn := range []*websocket.Conn{first, second} {
		if receive(t, conn).obj("item").str("path") != "/broadcast" {
			t.Fatal("every client must receive the event")
		}
	}
}
