package app

import (
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestAnyVerbIsEchoed(t *testing.T) {
	a := start(t, nil)
	for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		echo := a.echo(t, request{method: verb, path: "/qualquer/coisa"})
		if echo.str("Method") != verb || echo.str("Path") != "/qualquer/coisa" || echo.str("AppName") != "Echo Tests" {
			t.Errorf("%s: %v", verb, echo)
		}
	}
}

func TestAnyPathIsEchoed(t *testing.T) {
	a := start(t, nil)
	for _, path := range []string{"/", "/a/b/c", "/arquivo.txt", "/api/v1/users/42.json", "/com%20espa%C3%A7o"} {
		echo := a.echo(t, request{path: path})
		want, _ := url.PathUnescape(path)
		if echo.str("Path") != want {
			t.Errorf("%s: Path = %q, want %q", path, echo.str("Path"), want)
		}
	}
}

func TestHeadReturns200WithoutBody(t *testing.T) {
	a := start(t, nil)
	response, body := a.send(t, request{method: http.MethodHead, path: "/head"})
	if response.StatusCode != http.StatusOK || len(body) != 0 || response.Header.Get("X-Echo-Request-Id") == "" {
		t.Fatalf("status %d, %d bytes, headers %v", response.StatusCode, len(body), response.Header)
	}
}

func TestNonHTMLAcceptReturnsJSON(t *testing.T) {
	a := start(t, nil)
	for _, accept := range []string{"*/*", "application/json", "application/json, text/plain, */*", "", "text/html;q=0"} {
		header := http.Header{}
		if accept != "" {
			header.Set("Accept", accept)
		}
		response, _ := a.send(t, request{path: "/negociacao", header: header})
		if response.StatusCode != http.StatusOK || mediaType(response) != "application/json" {
			t.Errorf("Accept %q: %d %s", accept, response.StatusCode, mediaType(response))
		}
	}
}

func TestBrowserAcceptReturnsHTML(t *testing.T) {
	a := start(t, nil)
	for _, accept := range []string{"text/html", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", "application/xhtml+xml"} {
		response, body := a.send(t, request{path: "/pagina?x=1", header: http.Header{"Accept": {accept}}})
		html := string(body)
		if response.StatusCode != http.StatusOK || mediaType(response) != "text/html" ||
			!strings.Contains(html, "Echo Tests") || !strings.Contains(html, "/pagina") {
			t.Errorf("Accept %q: %d %s", accept, response.StatusCode, mediaType(response))
		}
	}
}

func TestHTMLIsEscaped(t *testing.T) {
	a := start(t, nil)
	_, body := a.send(t, request{
		method: http.MethodPost, path: "/html-post", contentType: "application/json",
		body:   strings.NewReader(`{"nome":"<script>alert(1)</script>"}`),
		header: http.Header{"Accept": {"text/html"}},
	})
	html := string(body)
	if strings.Contains(html, "<script>alert(1)</script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("the body must be HTML-escaped")
	}
}

func TestQueryStringWithRepeatedKeys(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{path: "/q?a=1&a=2&b=x%20y"})

	query := map[string][]string{}
	for _, item := range echo.arr("QueryString") {
		kv := obj(item.(map[string]any))
		query[kv.str("Key")] = kv.strs("Value")
	}
	if !slices.Equal(query["a"], []string{"1", "2"}) || !slices.Equal(query["b"], []string{"x y"}) {
		t.Fatalf("query = %v", query)
	}
}

func TestCustomHeadersAreEchoed(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{path: "/headers", header: http.Header{"X-Correlation-Id": {"abc-123"}}})

	headers := echo.obj("Headers")
	if headers.strs("X-Correlation-Id")[0] != "abc-123" {
		t.Fatalf("headers = %v", headers)
	}
	if !strings.HasPrefix(headers.strs("Host")[0], "127.0.0.1:") {
		t.Fatalf("the Host header must be echoed: %v", headers)
	}
}

func TestForwardedHeadersAreApplied(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{path: "/forwarded", header: http.Header{
		"X-Forwarded-Proto":  {"https"},
		"X-Forwarded-Host":   {"api.exemplo.com"},
		"X-Forwarded-For":    {"203.0.113.7"},
		"X-Forwarded-Prefix": {"/app"},
	}})

	if echo.str("Scheme") != "https" || echo.str("Host") != "api.exemplo.com" || echo.str("RemoteIp") != "203.0.113.7" || echo.str("PathBase") != "/app" {
		t.Fatalf("echo = %v", echo)
	}
	headers := echo.obj("Headers")
	if headers.strs("X-Original-Proto")[0] != "http" || !strings.HasPrefix(headers.strs("X-Original-For")[0], "127.0.0.1:") {
		t.Fatalf("original values must stay visible: %v", headers)
	}
}

func TestEnvironmentAndMachineName(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{path: "/env"})
	hostname, _ := os.Hostname()
	if echo.obj("EnvironmentVariables") == nil || echo.str("MachineName") != hostname {
		t.Fatalf("echo = %v", echo)
	}
}

func TestEchoFieldOrder(t *testing.T) {
	a := start(t, nil)
	_, body := a.send(t, request{method: http.MethodPost, path: "/ordem", contentType: "application/json", body: strings.NewReader(`{"a":1}`)})

	order := []string{`"RequestId"`, `"AppName"`, `"Now"`, `"UtcNow"`, `"Headers"`, `"Path"`, `"PathBase"`, `"MachineName"`,
		`"Method"`, `"Scheme"`, `"Host"`, `"Protocol"`, `"RemoteIp"`, `"Problems"`, `"QueryString"`, `"Forms"`,
		`"Payload"`, `"Body"`, `"EnvironmentVariables"`}
	last := -1
	text := string(body)
	for _, field := range order {
		index := strings.Index(text, "\n  "+field+":")
		if index <= last {
			t.Fatalf("%s out of order in:\n%s", field, text)
		}
		last = index
	}
}
