package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func html(t *testing.T, a *testApp) string {
	t.Helper()
	response, body := a.send(t, request{path: "/pagina", header: http.Header{"Accept": {"text/html"}}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	return string(body)
}

func TestEnvironmentIsShown(t *testing.T) {
	for _, value := range []string{"", "true", "1", "SIM"} {
		a := start(t, map[string]string{"ECHO_SHOW_ENVIRONMENT": value})
		if echo := a.echo(t, request{path: "/env"}); !echo.has("EnvironmentVariables") {
			t.Errorf("%q: EnvironmentVariables missing", value)
		}
		if !strings.Contains(html(t, a), "Environment Variables") {
			t.Errorf("%q: HTML without environment", value)
		}
	}
}

func TestEnvironmentIsHidden(t *testing.T) {
	hostname, _ := os.Hostname()
	for _, value := range []string{"false", "0", "no", "Off"} {
		a := start(t, map[string]string{"ECHO_SHOW_ENVIRONMENT": value})
		echo := a.echo(t, request{path: "/env"})
		if echo.has("EnvironmentVariables") || echo.str("MachineName") != hostname {
			t.Errorf("%q: %v", value, echo)
		}
		page := html(t, a)
		if strings.Contains(page, "Environment Variables") || strings.Contains(page, os.Getenv("PATH")) {
			t.Errorf("%q: HTML shows the environment", value)
		}
	}
}
