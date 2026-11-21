package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
)

func TestJSONIsParsedForAnyVerbWithBody(t *testing.T) {
	a := start(t, nil)
	for _, verb := range []string{"POST", "PUT", "PATCH", "DELETE", "GET"} {
		echo := a.echo(t, request{method: verb, path: "/json", contentType: "application/json", body: strings.NewReader(`{"id":1,"nome":"Ana"}`)})
		if echo.body().str("Kind") != "json" || echo.obj("Payload").num("id") != 1 || echo.obj("Payload").str("nome") != "Ana" {
			t.Errorf("%s: %v", verb, echo)
		}
	}
}

func TestJSONWithCharsetIsParsed(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/json", contentType: "application/json; charset=utf-8", body: strings.NewReader(`{"acentuação":"é"}`)})
	if echo.obj("Payload").str("acentuação") != "é" {
		t.Fatalf("payload = %v", echo["Payload"])
	}
}

func TestJSONNonObjectRoots(t *testing.T) {
	a := start(t, nil)
	cases := map[string]func(any) bool{
		`[1,2,3]`:     func(v any) bool { _, ok := v.([]any); return ok },
		`42`:          func(v any) bool { return v != nil },
		`  "texto"  `: func(v any) bool { return v == "texto" },
		`null`:        func(v any) bool { return v == nil },
	}
	for body, check := range cases {
		echo := a.echo(t, request{method: "POST", path: "/json", contentType: "application/json", body: strings.NewReader(body)})
		if echo.body().str("Kind") != "json" || !echo.has("Payload") || !check(echo["Payload"]) {
			t.Errorf("%s: %v", body, echo["Payload"])
		}
	}
}

func TestJSONMediaTypeVariants(t *testing.T) {
	a := start(t, nil)
	for _, mt := range []string{"application/problem+json", "application/vnd.api+json", "text/json"} {
		echo := a.echo(t, request{method: "POST", path: "/json", contentType: mt, body: strings.NewReader(`{"ok":true}`)})
		if !echo.obj("Payload").bool("ok") {
			t.Errorf("%s: %v", mt, echo)
		}
	}
}

func TestInvalidJSONDoesNotFail(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/json", contentType: "application/json", body: strings.NewReader("{ isto não é json")})
	body := echo.body()
	if body.str("Kind") != "json" || body.str("Text") != "{ isto não é json" || body.str("JsonError") == "" || echo.has("Payload") {
		t.Fatalf("echo = %v", echo)
	}
}

func TestXMLIsEchoedAsText(t *testing.T) {
	a := start(t, nil)
	const xml = `<?xml version="1.0"?><pedido id="7"><item>Café</item></pedido>`
	for _, mt := range []string{"application/xml", "text/xml", "application/soap+xml"} {
		echo := a.echo(t, request{method: "POST", path: "/xml", contentType: mt, body: strings.NewReader(xml)})
		if echo.body().str("Kind") != "xml" || echo.body().str("Text") != xml {
			t.Errorf("%s: %v", mt, echo.body())
		}
	}
}

func TestPlainTextFormats(t *testing.T) {
	a := start(t, nil)
	for _, mt := range []string{"text/plain", "text/csv", "application/x-ndjson", "application/javascript"} {
		echo := a.echo(t, request{method: "POST", path: "/texto", contentType: mt + "; charset=utf-8", body: strings.NewReader("linha 1\nlinha 2 ç")})
		if echo.body().str("Kind") != "text" || echo.body().str("Text") != "linha 1\nlinha 2 ç" {
			t.Errorf("%s: %v", mt, echo.body())
		}
	}
}

func TestLatin1TextIsDecoded(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/latin1", contentType: "text/plain; charset=iso-8859-1", body: strings.NewReader("a\xe7\xe3o")})
	if echo.body().str("Text") != "ação" || echo.body().str("Charset") != "iso-8859-1" {
		t.Fatalf("body = %v", echo.body())
	}
}

func TestURLEncodedForm(t *testing.T) {
	a := start(t, nil)
	form := "nome=Ana+Maria&tag=a&tag=b"
	echo := a.echo(t, request{method: "POST", path: "/form", contentType: "application/x-www-form-urlencoded", body: strings.NewReader(form)})

	body := echo.body()
	fields := body.obj("Form")
	if body.str("Kind") != "form" || body.num("Size") != int64(len(form)) || fields.strs("nome")[0] != "Ana Maria" || len(fields.strs("tag")) != 2 {
		t.Fatalf("body = %v", body)
	}
	found := false
	for _, item := range echo.arr("Forms") {
		found = found || item.(map[string]any)["Key"] == "nome"
	}
	if !found {
		t.Fatalf("legacy Forms = %v", echo["Forms"])
	}
}

func TestMultipartWithFieldsAndFiles(t *testing.T) {
	a := start(t, nil)
	fileBytes := make([]byte, 5000)
	_, _ = rand.Read(fileBytes)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("campo", "valor do campo")
	part, _ := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="arquivo"; filename="foto.png"`},
		"Content-Type":        {"image/png"},
	})
	_, _ = part.Write(fileBytes)
	doc, _ := writer.CreateFormFile("doc", "nota.txt")
	_, _ = io.WriteString(doc, "conteúdo txt")
	_ = writer.Close()

	echo := a.echo(t, request{method: "POST", path: "/upload", contentType: writer.FormDataContentType(), body: &buf})

	body := echo.body()
	if body.str("Kind") != "multipart" || body.num("Size") <= 5000 || body.obj("Form").strs("campo")[0] != "valor do campo" {
		t.Fatalf("body = %v", body)
	}
	files := body.arr("Files")
	if len(files) != 2 {
		t.Fatalf("files = %v", files)
	}
	foto := obj(files[0].(map[string]any))
	if foto.str("FileName") != "foto.png" || foto.str("Name") != "arquivo" || foto.str("ContentType") != "image/png" || foto.num("Length") != 5000 {
		t.Fatalf("foto = %v", foto)
	}
}

func TestMalformedMultipartDoesNotFail(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/upload-quebrado", contentType: "multipart/form-data; boundary=----nada", body: strings.NewReader("isto não é multipart")})
	if echo.body().str("Error") == "" || echo.body().str("Text") != "isto não é multipart" {
		t.Fatalf("body = %v", echo.body())
	}
}

func TestBinaryBodyIsEchoedAsBase64(t *testing.T) {
	a := start(t, nil)
	data := []byte{0x00, 0xFF, 0x10, 0x80, 0x7F}
	echo := a.echo(t, request{method: "PUT", path: "/binario", contentType: "application/octet-stream", body: bytes.NewReader(data)})

	decoded, _ := base64.StdEncoding.DecodeString(echo.body().str("Base64"))
	if echo.body().str("Kind") != "binary" || echo.body().num("Size") != 5 || !bytes.Equal(decoded, data) {
		t.Fatalf("body = %v", echo.body())
	}
}

func TestBodyWithoutContentTypeIsDetected(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/sem-content-type", body: strings.NewReader(`{"semTipo":true}`)})
	if echo.body().str("Kind") != "json" || !echo.obj("Payload").bool("semTipo") {
		t.Fatalf("echo = %v", echo)
	}
}

func TestEmptyBodyWithContentType(t *testing.T) {
	a := start(t, nil)
	echo := a.echo(t, request{method: "POST", path: "/vazio", contentType: "application/json", body: strings.NewReader("")})
	if echo.body().str("Kind") != "none" || echo.body().num("Size") != 0 {
		t.Fatalf("body = %v", echo.body())
	}
}

func TestChunkedBodyIsFullyRead(t *testing.T) {
	a := start(t, nil)
	items := make([]string, 2000)
	for i := range items {
		items[i] = "1"
	}
	payload := `{"itens":[` + strings.Join(items, ",") + `]}`

	echo := a.echo(t, request{method: "POST", path: "/chunked", contentType: "application/json", body: io.NopCloser(strings.NewReader(payload)), chunked: true})

	if len(echo.obj("Payload").arr("itens")) != 2000 || echo.body().num("Size") != int64(len(payload)) {
		t.Fatalf("size = %d", echo.body().num("Size"))
	}
	if echo.obj("Headers").strs("Transfer-Encoding")[0] != "chunked" {
		t.Fatalf("headers = %v", echo["Headers"])
	}
}

func TestFormInHistoryKeepsFieldsAndFiles(t *testing.T) {
	a := start(t, nil)
	values := url.Values{"x": {"1"}}
	echo := a.echo(t, request{method: "POST", path: "/f", contentType: "application/x-www-form-urlencoded", body: strings.NewReader(values.Encode())})

	detail := a.getJSON(t, fmt.Sprintf("/_echo/api/requests/%d", echo.num("RequestId")))
	if detail.obj("body").obj("form").strs("x")[0] != "1" {
		t.Fatalf("detail = %v", detail)
	}
}
