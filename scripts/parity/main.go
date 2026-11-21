// Command parity envia os mesmos requests para duas instâncias do echo (a referência .NET e a
// reimplementação em Go) e compara as respostas: status, headers X-Echo-*, o JSON do echo e o
// detalhe/resumo do histórico.
//
// Campos que mudam por natureza (horários, hostname, IPs, ambiente) e mensagens geradas pelos
// parsers (JSON, multipart) são ignorados.
//
//	go run ./scripts/parity -a http://echo-dotnet -b http://echo-go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"reflect"
	"sort"
	"strings"
)

type testCase struct {
	name        string
	method      string
	path        string
	contentType string
	header      map[string]string
	body        []byte
	chunked     bool
}

func cases() []testCase {
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	_ = writer.SetBoundary("parity-boundary")
	_ = writer.WriteField("campo", "valor do campo")
	part, _ := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="arquivo"; filename="foto.png"`},
		"Content-Type":        {"image/png"},
	})
	_, _ = part.Write(bytes.Repeat([]byte{0x89, 0x50}, 500))
	_ = writer.Close()

	return []testCase{
		{name: "readme example", method: "POST", path: "/orders?source=readme", contentType: "application/json",
			header: map[string]string{"X-Correlation-Id": "abc-123"}, body: []byte(`{"customer":"Ana","items":[1,2]}`)},
		{name: "get", method: "GET", path: "/a/b/c"},
		{name: "encoded path", method: "GET", path: "/com%20espa%C3%A7o"},
		{name: "repeated query", method: "GET", path: "/q?a=1&a=2&b=x%20y&vazio="},
		{name: "delete with body", method: "DELETE", path: "/json", contentType: "application/json", body: []byte(`{"id":1}`)},
		{name: "json array", method: "POST", path: "/json", contentType: "application/json", body: []byte(`[1,2,3]`)},
		{name: "json null", method: "POST", path: "/json", contentType: "application/json", body: []byte(`null`)},
		{name: "json string", method: "POST", path: "/json", contentType: "application/json", body: []byte(`  "texto"  `)},
		{name: "problem+json", method: "POST", path: "/json", contentType: "application/problem+json; charset=utf-8", body: []byte(`{"ok":true}`)},
		{name: "invalid json", method: "POST", path: "/json", contentType: "application/json", body: []byte(`{ "a": `)},
		{name: "xml", method: "POST", path: "/xml", contentType: "application/soap+xml", body: []byte(`<?xml version="1.0"?><a>Café</a>`)},
		{name: "text", method: "POST", path: "/texto", contentType: "text/plain; charset=utf-8", body: []byte("linha 1\nlinha 2 ç")},
		{name: "ndjson", method: "POST", path: "/texto", contentType: "application/x-ndjson", body: []byte("{}\n{}")},
		{name: "latin1", method: "POST", path: "/latin1", contentType: "text/plain; charset=iso-8859-1", body: []byte("a\xe7\xe3o")},
		{name: "charset mismatch", method: "POST", path: "/charset", contentType: "text/plain; charset=utf-8", body: []byte("a\xe7\xe3o")},
		{name: "unknown charset", method: "POST", path: "/charset", contentType: "text/plain; charset=klingon", body: []byte("olá")},
		{name: "bom", method: "POST", path: "/bom", contentType: "text/plain", body: []byte("\xef\xbb\xbfolá")},
		{name: "invalid content-type", method: "POST", path: "/ct", contentType: "isto nao e um content type", body: []byte(`{"ok":true}`)},
		{name: "no content-type json", method: "POST", path: "/sniff", body: []byte(`{"semTipo":true}`)},
		{name: "no content-type text", method: "POST", path: "/sniff", body: []byte("só texto")},
		{name: "no content-type binary", method: "POST", path: "/sniff", body: []byte{0, 1, 2, 3}},
		{name: "binary", method: "PUT", path: "/binario", contentType: "application/octet-stream", body: []byte{0x00, 0xFF, 0x10, 0x80, 0x7F}},
		{name: "empty json", method: "POST", path: "/vazio", contentType: "application/json", body: []byte{}},
		{name: "form", method: "POST", path: "/form", contentType: "application/x-www-form-urlencoded", body: []byte("nome=Ana+Maria&tag=a&tag=b")},
		{name: "multipart", method: "POST", path: "/upload", contentType: writer.FormDataContentType(), body: multipartBody.Bytes()},
		{name: "malformed multipart", method: "POST", path: "/upload", contentType: "multipart/form-data; boundary=xyz", body: []byte("sem boundary nenhum")},
		{name: "multipart without boundary", method: "POST", path: "/upload", contentType: "multipart/form-data", body: []byte("abc")},
		{name: "chunked", method: "POST", path: "/chunked", contentType: "application/json", body: []byte(`{"itens":[1,2,3]}`), chunked: true},
		{name: "big text (truncated)", method: "POST", path: "/grande", contentType: "text/plain", body: bytes.Repeat([]byte("x"), 70_000)},
		{name: "big json (truncated)", method: "POST", path: "/grande", contentType: "application/json", body: []byte(`{"a":"` + strings.Repeat("y", 70_000) + `"}`)},
		{name: "forwarded", method: "GET", path: "/forwarded", header: map[string]string{
			"X-Forwarded-Proto": "https", "X-Forwarded-Host": "api.exemplo.com", "X-Forwarded-For": "203.0.113.7, 198.51.100.2", "X-Forwarded-Prefix": "/app"}},
		{name: "invalid forwarded for", method: "GET", path: "/forwarded", header: map[string]string{"X-Forwarded-For": "nao-e-ip", "X-Forwarded-Proto": "https"}},
		{name: "head", method: "HEAD", path: "/head"},
		{name: "options", method: "OPTIONS", path: "/options"},
		{name: "html accept", method: "GET", path: "/pagina", header: map[string]string{"Accept": "text/html"}},
		{name: "html q=0", method: "GET", path: "/pagina", header: map[string]string{"Accept": "text/html;q=0, application/json"}},
	}
}

// Campos ignorados no echo (PascalCase) e na API (camelCase).
var ignored = map[string]bool{
	"Now": true, "UtcNow": true, "MachineName": true, "EnvironmentVariables": true, "RequestId": true, "RemoteIp": true,
	"id": true, "timestamp": true, "remoteIp": true,
	// mensagens geradas pelos parsers de JSON e multipart
	"JsonError": true, "jsonError": true, "Error": true, "error": true,
}

var ignoredHeaders = map[string]bool{"X-Original-For": true}

func main() {
	a := flag.String("a", "http://echo-dotnet", "reference instance")
	b := flag.String("b", "http://echo-go", "instance under test")
	flag.Parse()

	failures := 0
	for _, c := range cases() {
		ra, err := run(*a, c)
		if err != nil {
			fmt.Printf("ERROR %s (%s): %v\n", c.name, *a, err)
			failures++
			continue
		}
		rb, err := run(*b, c)
		if err != nil {
			fmt.Printf("ERROR %s (%s): %v\n", c.name, *b, err)
			failures++
			continue
		}
		diffs := compare(ra, rb)
		if len(diffs) == 0 {
			fmt.Printf("ok    %s\n", c.name)
			continue
		}
		failures++
		fmt.Printf("DIFF  %s\n", c.name)
		for _, d := range diffs {
			fmt.Printf("        %s\n", d)
		}
	}
	fmt.Printf("\n%d case(s), %d with differences\n", len(cases()), failures)
	if failures > 0 {
		os.Exit(1)
	}
}

type outcome struct {
	status      int
	contentType string
	problems    string
	bodyLen     int
	echo        any // JSON do echo (nil para HTML/HEAD)
	detail      any // /_echo/api/requests/{id}
	summary     any // item correspondente em /_echo/api/requests
}

func run(base string, c testCase) (*outcome, error) {
	var body io.Reader
	if c.body != nil {
		body = bytes.NewReader(c.body)
		if c.chunked {
			body = io.NopCloser(body)
		}
	}
	req, err := http.NewRequest(c.method, base+c.path, body)
	if err != nil {
		return nil, err
	}
	req.Host = "echo.test"
	req.Header.Set("User-Agent", "parity")
	req.Header.Set("Accept", "application/json")
	if c.contentType != "" {
		req.Header.Set("Content-Type", c.contentType)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	if c.chunked {
		req.ContentLength = -1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	mt, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	o := &outcome{status: resp.StatusCode, contentType: strings.TrimSpace(mt), problems: resp.Header.Get("X-Echo-Problems"), bodyLen: len(data)}
	if o.contentType == "application/json" && len(data) > 0 {
		if err := json.Unmarshal(data, &o.echo); err != nil {
			return nil, fmt.Errorf("echo is not JSON: %w", err)
		}
	}
	if o.contentType != "text/html" {
		o.bodyLen = 0 // o tamanho do JSON varia com horários e ambiente
	}

	id := resp.Header.Get("X-Echo-Request-Id")
	if id == "" {
		return o, nil
	}
	if o.detail, err = getJSON(base + "/_echo/api/requests/" + id); err != nil {
		return nil, err
	}
	list, err := getJSON(base + "/_echo/api/requests?limit=1")
	if err != nil {
		return nil, err
	}
	if items, _ := list.(map[string]any)["items"].([]any); len(items) > 0 {
		o.summary = items[0]
	}
	return o, nil
}

func getJSON(url string) (any, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var v any
	return v, json.NewDecoder(resp.Body).Decode(&v)
}

func compare(a, b *outcome) []string {
	var diffs []string
	if a.status != b.status {
		diffs = append(diffs, fmt.Sprintf("status: %d != %d", a.status, b.status))
	}
	if a.contentType != b.contentType {
		diffs = append(diffs, fmt.Sprintf("content-type: %q != %q", a.contentType, b.contentType))
	}
	if a.problems != b.problems {
		diffs = append(diffs, fmt.Sprintf("X-Echo-Problems: %q != %q", a.problems, b.problems))
	}
	diffs = append(diffs, diff("echo", a.echo, b.echo)...)
	diffs = append(diffs, diff("detail", a.detail, b.detail)...)
	diffs = append(diffs, diff("summary", a.summary, b.summary)...)
	return diffs
}

func diff(path string, a, b any) []string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: %s != %s", path, short(a), short(b))}
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		var sorted []string
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)

		var diffs []string
		for _, k := range sorted {
			if ignored[k] || (strings.HasSuffix(path, ".Headers") || strings.HasSuffix(path, ".headers")) && ignoredHeaders[k] {
				continue
			}
			if isProblemMessage(path, k, av) {
				continue
			}
			x, inA := av[k]
			y, inB := bv[k]
			switch {
			case !inA:
				diffs = append(diffs, fmt.Sprintf("%s.%s: only in b = %s", path, k, short(y)))
			case !inB:
				diffs = append(diffs, fmt.Sprintf("%s.%s: only in a = %s", path, k, short(x)))
			default:
				diffs = append(diffs, diff(path+"."+k, x, y)...)
			}
		}
		return diffs
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return []string{fmt.Sprintf("%s: %s != %s", path, short(a), short(b))}
		}
		var diffs []string
		for i := range av {
			diffs = append(diffs, diff(fmt.Sprintf("%s[%d]", path, i), av[i], bv[i])...)
		}
		return diffs
	default:
		if !reflect.DeepEqual(a, b) {
			return []string{fmt.Sprintf("%s: %s != %s", path, short(a), short(b))}
		}
		return nil
	}
}

// As mensagens de json_invalid e form_invalid vêm dos parsers de cada plataforma.
func isProblemMessage(path, key string, problem map[string]any) bool {
	if key != "Message" && key != "message" {
		return false
	}
	code, _ := problem["Code"].(string)
	if code == "" {
		code, _ = problem["code"].(string)
	}
	return code == "json_invalid" || code == "form_invalid"
}

func short(v any) string {
	data, _ := json.Marshal(v)
	if len(data) > 160 {
		return string(data[:160]) + "…"
	}
	return string(data)
}
