package config

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDefaults(t *testing.T) {
	options, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Options{
		AppName: "Echo", HeaderColor: "gray-900", ShowEnvironment: true,
		MaxRequests: 1000, MaxBodyBytes: 65536, MaxRequestBytes: 30_000_000, Port: 80,
	}
	if options != want {
		t.Fatalf("got %+v, want %+v", options, want)
	}
}

func TestValuesAreRead(t *testing.T) {
	options, err := Load(env(map[string]string{
		"APP_NAME": "  Echo 1 ", "HEADER_COLOR": "blue-600", "ECHO_SHOW_ENVIRONMENT": "off",
		"ECHO_MAX_REQUESTS": " 200 ", "ECHO_MAX_BODY_BYTES": "10", "ECHO_MAX_REQUEST_BYTES": "+1000", "ECHO_PORT": "8080",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Options{
		AppName: "Echo 1", HeaderColor: "blue-600", ShowEnvironment: false,
		MaxRequests: 200, MaxBodyBytes: 10, MaxRequestBytes: 1000, Port: 8080,
	}
	if options != want {
		t.Fatalf("got %+v, want %+v", options, want)
	}
}

func TestBooleans(t *testing.T) {
	for _, value := range []string{"true", "1", "yes", "y", "on", "sim", "s", "SIM", "True", " on "} {
		if options, err := Load(env(map[string]string{"ECHO_SHOW_ENVIRONMENT": value})); err != nil || !options.ShowEnvironment {
			t.Errorf("%q: got %v, %v; want true", value, options.ShowEnvironment, err)
		}
	}
	for _, value := range []string{"false", "0", "no", "n", "off", "nao", "não", "NÃO", "Off"} {
		if options, err := Load(env(map[string]string{"ECHO_SHOW_ENVIRONMENT": value})); err != nil || options.ShowEnvironment {
			t.Errorf("%q: got %v, %v; want false", value, options.ShowEnvironment, err)
		}
	}
	for _, value := range []string{"", "   "} {
		if options, err := Load(env(map[string]string{"ECHO_SHOW_ENVIRONMENT": value})); err != nil || !options.ShowEnvironment {
			t.Errorf("%q: got %v, %v; want the default (true)", value, options.ShowEnvironment, err)
		}
	}
}

func TestInvalidValuesFailOnStartup(t *testing.T) {
	cases := map[string][]string{
		"ECHO_SHOW_ENVIRONMENT":  {"talvez"},
		"ECHO_MAX_REQUESTS":      {"0", "-1", "abc", "1.5", "99999999999"},
		"ECHO_MAX_BODY_BYTES":    {"0", "x"},
		"ECHO_MAX_REQUEST_BYTES": {"-5"},
		"ECHO_PORT":              {"0", "70000"},
	}
	for key, values := range cases {
		for _, value := range values {
			_, err := Load(env(map[string]string{key: value}))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("%s=%q: got %v, want an error naming %s", key, value, err, key)
			}
		}
	}
}

func TestAllErrorsAreReported(t *testing.T) {
	_, err := Load(env(map[string]string{"ECHO_MAX_REQUESTS": "x", "ECHO_SHOW_ENVIRONMENT": "?"}))
	if err == nil || !strings.Contains(err.Error(), "ECHO_MAX_REQUESTS") || !strings.Contains(err.Error(), "ECHO_SHOW_ENVIRONMENT") {
		t.Fatalf("got %v", err)
	}
}
