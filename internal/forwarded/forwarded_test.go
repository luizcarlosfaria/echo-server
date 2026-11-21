package forwarded

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func run(t *testing.T, r *http.Request) (Info, http.Header, string) {
	t.Helper()
	var info Info
	var header http.Header
	var host string
	Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info, header, host = FromRequest(r), r.Header, r.Host
	})).ServeHTTP(httptest.NewRecorder(), r)
	return info, header, host
}

func TestWithoutForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost:81/x", nil)
	r.RemoteAddr = "[::ffff:172.17.0.1]:5000"

	info, header, _ := run(t, r)

	if info != (Info{Scheme: "http", Host: "localhost:81", RemoteIP: "172.17.0.1"}) {
		t.Fatalf("info = %+v", info)
	}
	if header.Get("Host") != "localhost:81" {
		t.Fatalf("the Host header must be visible, got %v", header)
	}
}

func TestForwardedHeadersAreApplied(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://interno:8080/x", nil)
	r.RemoteAddr = "10.0.0.5:4321"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "api.exemplo.com")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.Header.Set("X-Forwarded-Prefix", "/app")

	info, header, host := run(t, r)

	if info != (Info{Scheme: "https", Host: "api.exemplo.com", PathBase: "/app", RemoteIP: "203.0.113.7"}) {
		t.Fatalf("info = %+v", info)
	}
	if host != "api.exemplo.com" || header.Get("Host") != "api.exemplo.com" {
		t.Fatalf("host = %q, header = %q", host, header.Get("Host"))
	}
	want := map[string]string{
		"X-Original-For": "10.0.0.5:4321", "X-Original-Proto": "http", "X-Original-Host": "interno:8080",
	}
	for name, value := range want {
		if got := header.Values(name); len(got) != 1 || got[0] != value {
			t.Errorf("%s = %v, want %q", name, got, value)
		}
	}
	if _, ok := header["X-Original-Prefix"]; ok {
		t.Error("an empty original PathBase must not create X-Original-Prefix")
	}
	for _, name := range []string{"X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-For", "X-Forwarded-Prefix"} {
		if header.Get(name) != "" {
			t.Errorf("%s should have been consumed", name)
		}
	}
}

func TestOnlyTheRightmostEntryIsConsumed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Add("X-Forwarded-For", "198.51.100.1, 198.51.100.2")
	r.Header.Add("X-Forwarded-For", "203.0.113.9:7000")

	info, header, _ := run(t, r)

	if info.RemoteIP != "203.0.113.9" {
		t.Fatalf("remote ip = %q", info.RemoteIP)
	}
	if got := header.Get("X-Forwarded-For"); got != "198.51.100.1, 198.51.100.2" {
		t.Fatalf("remaining X-Forwarded-For = %q", got)
	}
}

func TestInvalidEntryCancelsEverything(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	r.Header.Set("X-Forwarded-Proto", "https")

	info, header, _ := run(t, r)

	if info.Scheme != "http" || header.Get("X-Forwarded-Proto") != "https" || header.Get("X-Original-Proto") != "" {
		t.Fatalf("nothing should be applied: %+v %v", info, header)
	}
}

func TestIPv6ForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "[2001:db8::1]:443")

	info, _, _ := run(t, r)

	if info.RemoteIP != "2001:db8::1" {
		t.Fatalf("remote ip = %q", info.RemoteIP)
	}
}

func TestTransferEncodingIsVisible(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.TransferEncoding = []string{"chunked"}

	_, header, _ := run(t, r)

	if header.Get("Transfer-Encoding") != "chunked" {
		t.Fatalf("header = %v", header)
	}
}
