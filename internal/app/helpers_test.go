package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luizcarlosfaria/echo-server/internal/config"
	"github.com/luizcarlosfaria/echo-server/internal/store"
)

// testApp sobe a aplicação num servidor HTTP real (httptest), como o WebApplicationFactory
// dos testes .NET. Variáveis extras simulam as variáveis de ambiente do container.
type testApp struct {
	server *httptest.Server
	store  *store.Store
}

func start(t *testing.T, settings map[string]string) *testApp {
	t.Helper()
	getenv := func(key string) string {
		if value, ok := settings[key]; ok {
			return value
		}
		if key == "APP_NAME" {
			return "Echo Tests"
		}
		return ""
	}
	options, err := config.Load(getenv)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(options.MaxRequests)
	stopping, stop := context.WithCancel(context.Background())
	server := httptest.NewServer(New(options, st, stopping))
	t.Cleanup(func() {
		stop()
		server.Close()
	})
	return &testApp{server: server, store: st}
}

type request struct {
	method      string
	path        string
	body        io.Reader
	contentType string
	header      http.Header
	chunked     bool
}

func (a *testApp) send(t *testing.T, req request) (*http.Response, []byte) {
	t.Helper()
	if req.method == "" {
		req.method = http.MethodGet
	}
	r, err := http.NewRequest(req.method, a.server.URL+req.path, req.body)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range req.header {
		r.Header[name] = values
	}
	if req.contentType != "" {
		r.Header.Set("Content-Type", req.contentType)
	}
	if req.chunked {
		r.ContentLength = -1
	}
	response, err := a.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

// echo envia o request pedindo JSON e devolve o corpo do echo já parseado.
func (a *testApp) echo(t *testing.T, req request) obj {
	t.Helper()
	if req.header == nil {
		req.header = http.Header{}
	}
	if req.header.Get("Accept") == "" {
		req.header.Set("Accept", "application/json")
	}
	response, body := a.send(t, req)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if mediaType(response) != "application/json" {
		t.Fatalf("content-type = %q", response.Header.Get("Content-Type"))
	}
	return parse(t, body)
}

func (a *testApp) getJSON(t *testing.T, path string) obj {
	t.Helper()
	response, body := a.send(t, request{path: path})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, response.StatusCode, body)
	}
	return parse(t, body)
}

// waitFor espera o request aparecer no histórico (requests cuja conexão caiu).
func (a *testApp) waitFor(t *testing.T, path string) obj {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, item := range a.getJSON(t, "/_echo/api/requests").arr("items") {
			if item.(map[string]any)["path"] == path {
				return obj(item.(map[string]any))
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not show up in the history", path)
	return nil
}

func mediaType(response *http.Response) string {
	value, _, _ := strings.Cut(response.Header.Get("Content-Type"), ";")
	return strings.TrimSpace(value)
}

// obj facilita navegar no JSON decodificado.
type obj map[string]any

func parse(t *testing.T, body []byte) obj {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	return value
}

func (o obj) has(key string) bool { _, ok := o[key]; return ok }

func (o obj) str(key string) string {
	value, ok := o[key].(string)
	if !ok {
		panic(fmt.Sprintf("%q is not a string: %#v", key, o[key]))
	}
	return value
}

func (o obj) num(key string) int64 {
	value, err := o[key].(json.Number).Int64()
	if err != nil {
		panic(fmt.Sprintf("%q is not an integer: %#v", key, o[key]))
	}
	return value
}

func (o obj) bool(key string) bool { return o[key].(bool) }

func (o obj) obj(key string) obj {
	value, ok := o[key].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("%q is not an object: %#v", key, o[key]))
	}
	return value
}

func (o obj) arr(key string) []any {
	value, ok := o[key].([]any)
	if !ok {
		panic(fmt.Sprintf("%q is not an array: %#v", key, o[key]))
	}
	return value
}

func (o obj) strs(key string) []string {
	var result []string
	for _, v := range o.arr(key) {
		result = append(result, v.(string))
	}
	return result
}

func (o obj) body() obj { return o.obj("Body") }

// problemCodes devolve os códigos de problema do echo (PascalCase).
func (o obj) problemCodes() []string {
	var result []string
	for _, p := range o.arr("Problems") {
		result = append(result, p.(map[string]any)["Code"].(string))
	}
	return result
}

func (o obj) problem(t *testing.T, code string) obj {
	t.Helper()
	for _, p := range o.arr("Problems") {
		if problem := obj(p.(map[string]any)); problem.str("Code") == code {
			return problem
		}
	}
	t.Fatalf("problem %q not reported; got %v", code, o.problemCodes())
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
