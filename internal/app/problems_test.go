package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanRequestHasNoProblems(t *testing.T) {
	a := start(t, nil)
	response, body := a.send(t, request{method: "POST", path: "/ok", contentType: "application/json", body: strings.NewReader(`{"a":1}`)})
	echo := parse(t, body)
	if len(echo.arr("Problems")) != 0 || response.Header.Get("X-Echo-Problems") != "" {
		t.Fatalf("problems = %v", echo["Problems"])
	}
}

func TestInvalidJSONIsFlaggedAsError(t *testing.T) {
	a := start(t, nil)
	response, body := a.send(t, request{method: "POST", path: "/json", contentType: "application/json", body: strings.NewReader(`{ "a": `)})
	echo := parse(t, body)

	problem := echo.problem(t, "json_invalid")
	if response.StatusCode != http.StatusOK || problem.str("Severity") != "error" || !strings.Contains(problem.str("Message"), "Invalid JSON") {
		t.Fatalf("problem = %v", problem)
	}
	if response.Header.Get("X-Echo-Problems") != "json_invalid" {
		t.Fatalf("X-Echo-Problems = %q", response.Header.Get("X-Echo-Problems"))
	}
}

func TestMalformedMultipartIsFlagged(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/upload", contentType: "multipart/form-data; boundary=xyz", body: strings.NewReader("sem boundary nenhum")})
	if echo.problem(t, "form_invalid").str("Severity") != "error" || echo.body().str("Text") != "sem boundary nenhum" {
		t.Fatalf("echo = %v", echo)
	}
}

func TestMultipartWithoutBoundaryIsFlagged(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/upload", contentType: "multipart/form-data", body: strings.NewReader("abc")})
	echo.problem(t, "form_invalid")
}

func TestUnparseableContentTypeIsFlaggedAndSniffed(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/ct", contentType: "isto nao e um content type", body: strings.NewReader(`{"ok":true}`)})
	if echo.problem(t, "content_type_invalid").str("Severity") != "warning" || !echo.obj("Payload").bool("ok") {
		t.Fatalf("echo = %v", echo)
	}
}

func TestUnknownCharsetIsFlaggedAndDecodedAsUTF8(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/charset", contentType: "text/plain; charset=klingon", body: strings.NewReader("olá")})
	echo.problem(t, "charset_unknown")
	if echo.body().str("Text") != "olá" {
		t.Fatalf("text = %q", echo.body().str("Text"))
	}
}

func TestBytesInvalidForDeclaredCharsetAreFlagged(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/charset", contentType: "text/plain; charset=utf-8", body: strings.NewReader("a\xe7\xe3o")})
	echo.problem(t, "charset_mismatch")
	if !strings.ContainsRune(echo.body().str("Text"), utf8.RuneError) {
		t.Fatalf("text = %q", echo.body().str("Text"))
	}
}

func TestTruncatedStorageIsFlaggedAsInfo(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_BODY_BYTES": "10"})
	echo := a.echo(t, request{method: "POST", path: "/grande", contentType: "text/plain; charset=utf-8", body: strings.NewReader(strings.Repeat("a", 100))})
	if echo.problem(t, "body_truncated").str("Severity") != "info" {
		t.Fatalf("echo = %v", echo)
	}
}

func TestOversizedBodyIsAcceptedPartiallyInsteadOf413(t *testing.T) {
	a := start(t, map[string]string{"ECHO_MAX_REQUEST_BYTES": "1000"})
	for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=abc"} {
		echo := a.echo(t, request{method: "POST", path: "/enorme", contentType: contentType, body: strings.NewReader(strings.Repeat("a", 5000))})
		if echo.problem(t, "body_too_large").str("Severity") != "warning" || echo.body().num("Size") != 1000 {
			t.Errorf("%s: %v", contentType, echo.body())
		}
		if contains(echo.problemCodes(), "content_length_mismatch") {
			t.Errorf("%s: unexpected content_length_mismatch", contentType)
		}
	}
}

func TestMultipleProblemsAreReportedTogether(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/varios", contentType: "application/json; charset=utf-8", body: strings.NewReader("{ a\xe7\xe3o")})
	codes := echo.problemCodes()
	if !contains(codes, "charset_mismatch") || !contains(codes, "json_invalid") {
		t.Fatalf("codes = %v", codes)
	}
}

func TestProblemsAreExposedToTheDashboard(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/painel", contentType: "application/json", body: strings.NewReader("{x")})

	summary := obj(a.getJSON(t, "/_echo/api/requests").arr("items")[0].(map[string]any))
	if strings.Join(summary.strs("problems"), ",") != "json_invalid" || summary.str("problemSeverity") != "error" {
		t.Fatalf("summary = %v", summary)
	}

	detail := a.getJSON(t, fmt.Sprintf("/_echo/api/requests/%d", echo.num("RequestId")))
	if obj(detail.arr("problems")[0].(map[string]any)).str("code") != "json_invalid" {
		t.Fatalf("detail = %v", detail)
	}
}

func TestCleanSummaryHasNoSeverity(t *testing.T) {
	a := start(t, nil)
	a.echo(t, request{path: "/limpo"})

	summary := obj(a.getJSON(t, "/_echo/api/requests").arr("items")[0].(map[string]any))
	if len(summary.arr("problems")) != 0 || summary.has("problemSeverity") {
		t.Fatalf("summary = %v", summary)
	}
}

func TestHTMLPageListsProblems(t *testing.T) {
	a := start(t, nil)
	response, body := a.send(t, request{method: "POST", path: "/html", contentType: "application/json", body: strings.NewReader("{x"), header: http.Header{"Accept": {"text/html"}}})
	html := string(body)
	if response.StatusCode != http.StatusOK || !strings.Contains(html, "Problems detected") || !strings.Contains(html, "json_invalid") {
		t.Fatalf("status %d", response.StatusCode)
	}
}
