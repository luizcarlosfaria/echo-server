package app

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Falhas que só existem numa conexão de verdade: o teste conversa com o servidor por TCP cru.

func connect(t *testing.T, a *testApp) *net.TCPConn {
	t.Helper()
	conn, err := net.Dial("tcp", a.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return conn.(*net.TCPConn)
}

func TestBodyShorterThanContentLengthIsKeptAsIncomplete(t *testing.T) {
	a := start(t, nil)
	conn := connect(t, a)
	_, _ = io.WriteString(conn, "POST /incompleto HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\n"+
		"Content-Type: text/plain\r\nContent-Length: 100\r\n\r\napenas 20 bytes aqui")
	time.Sleep(300 * time.Millisecond)
	_ = conn.CloseWrite() // FIN antes de completar o Content-Length

	// Não existe resposta possível: a conexão é fechada.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	response, _ := io.ReadAll(conn)
	conn.Close()
	if len(response) != 0 {
		t.Fatalf("unexpected response: %q", response)
	}

	// ...mas o request não se perde: fica no histórico, com o que chegou e o problema.
	summary := a.waitFor(t, "/incompleto")
	if !contains(summary.strs("problems"), "body_incomplete") || summary.str("problemSeverity") != "error" {
		t.Fatalf("summary = %v", summary)
	}
	detail := a.getJSON(t, "/_echo/api/requests/1")
	message := obj(detail.arr("problems")[0].(map[string]any)).str("message")
	if detail.obj("body").str("text") != "apenas 20 bytes aqui" || !strings.Contains(message, "after 20 bytes") {
		t.Fatalf("detail = %v", detail)
	}
}

func TestAbortedUploadIsKeptAsClientAborted(t *testing.T) {
	a := start(t, nil)
	conn := connect(t, a)
	_, _ = io.WriteString(conn, "POST /abortado HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 5000\r\n\r\n{\"parcial\":")
	time.Sleep(200 * time.Millisecond) // garante que o servidor começou a ler o body
	_ = conn.SetLinger(0)
	conn.Close() // RST: queda abrupta

	stored := a.waitFor(t, "/abortado")
	problems := stored.strs("problems")
	if !contains(problems, "client_aborted") && !contains(problems, "body_incomplete") {
		t.Fatalf("problems = %v", problems)
	}
	if stored.str("problemSeverity") != "error" || stored.num("bodySize") != 11 {
		t.Fatalf("stored = %v", stored)
	}
}

func TestBodyOverLimitGets200InsteadOf413(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_REQUEST_BYTES": "1024"})
	response, err := a.server.Client().Post(a.server.URL+"/upload-gigante", "", bytes.NewReader(make([]byte, 256*1024)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()

	echo := parse(t, body)
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("X-Echo-Problems"), "body_too_large") || echo.body().num("Size") != 1024 {
		t.Fatalf("status %d, problems %q, size %v", response.StatusCode, response.Header.Get("X-Echo-Problems"), echo.body()["Size"])
	}
}

func TestBodyLargerThanOldKestrelDefaultIsAccepted(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_REQUEST_BYTES": "40000000"})
	req, _ := http.NewRequest(http.MethodPut, a.server.URL+"/31mb", bytes.NewReader(make([]byte, 31_000_000)))
	req.Header.Set("Accept", "application/json")
	response, err := a.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()

	echo := parse(t, body)
	if response.StatusCode != http.StatusOK || echo.body().num("Size") != 31_000_000 || contains(echo.problemCodes(), "body_too_large") {
		t.Fatalf("status %d, size %v, problems %v", response.StatusCode, echo.body()["Size"], echo.problemCodes())
	}
}
