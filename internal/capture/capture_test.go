package capture

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luizcarlosfaria/echo-server/internal/forwarded"
	"github.com/luizcarlosfaria/echo-server/internal/model"
)

var defaultLimits = Limits{MaxBodyBytes: 64 * 1024, MaxRequestBytes: 30_000_000}

func captureBody(t *testing.T, body io.Reader, contentType string, limits Limits) Result {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return Capture(r, forwarded.FromRequest(r), limits, nil)
}

func codes(problems []model.Problem) []string {
	var result []string
	for _, p := range problems {
		result = append(result, p.Code)
	}
	return result
}

func hasProblem(problems []model.Problem, code string) *model.Problem {
	for i := range problems {
		if problems[i].Code == code {
			return &problems[i]
		}
	}
	return nil
}

// O net/http não deixa um cliente mentir o Content-Length; o teste monta o request à mão.
func TestContentLengthMismatch(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/cl", strings.NewReader("0123456789"))
	r.Header.Set("Content-Type", "text/plain")
	r.Header.Set("Content-Length", "25")

	result := Capture(r, forwarded.FromRequest(r), defaultLimits, nil)

	p := hasProblem(result.Request.Problems, model.ContentLengthMismatch)
	if p == nil || !strings.Contains(p.Message, "25") || p.Severity != model.SeverityError {
		t.Fatalf("problems = %+v", result.Request.Problems)
	}
	if result.Request.Body.Size != 10 || *result.Request.ContentLength != 25 {
		t.Fatalf("size = %d", result.Request.Body.Size)
	}
}

type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestUnexpectedEOFIsBodyIncomplete(t *testing.T) {
	result := captureBody(t, &failingReader{data: []byte("apenas 20 bytes aqui"), err: io.ErrUnexpectedEOF}, "text/plain", defaultLimits)

	p := hasProblem(result.Request.Problems, model.BodyIncomplete)
	if p == nil || !strings.Contains(p.Message, "after 20 bytes") || !result.Broken {
		t.Fatalf("problems = %+v broken = %v", result.Request.Problems, result.Broken)
	}
	if *result.Request.Body.Text != "apenas 20 bytes aqui" {
		t.Fatalf("text = %q", *result.Request.Body.Text)
	}
	if hasProblem(result.Request.Problems, model.ContentLengthMismatch) != nil {
		t.Fatal("a failed read must not also report content_length_mismatch")
	}
}

func TestConnectionResetIsClientAborted(t *testing.T) {
	reset := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}
	result := captureBody(t, &failingReader{data: []byte(`{"parcial":`), err: reset}, "application/json", defaultLimits)

	if hasProblem(result.Request.Problems, model.ClientAborted) == nil || result.Request.Body.Size != 11 {
		t.Fatalf("problems = %+v size = %d", result.Request.Problems, result.Request.Body.Size)
	}
	if result.Request.Body.JSONError == nil || result.Request.Body.JSON != nil {
		t.Fatal("an incomplete JSON must not be parsed")
	}
}

func TestClassification(t *testing.T) {
	cases := []struct {
		contentType, body, kind string
	}{
		{"application/json", `{"a":1}`, model.KindJSON},
		{"application/problem+json", `{"a":1}`, model.KindJSON},
		{"text/json", `{"a":1}`, model.KindJSON},
		{"application/soap+xml", `<a/>`, model.KindXML},
		{"text/xml", `<a/>`, model.KindXML},
		{"text/csv", "a,b", model.KindText},
		{"application/x-yaml", "a: 1", model.KindText},
		{"application/graphql", "{ a }", model.KindText},
		{"application/octet-stream", "abc", model.KindBinary},
		{"image/png", "abc", model.KindBinary},
		{"", `{"a":1}`, model.KindJSON},
		{"", `[1,2]`, model.KindJSON},
		{"", `{nope`, model.KindText},
		{"", "olá", model.KindText},
		{"", "\x00\x01", model.KindBinary},
		{"", "\xff\xfe", model.KindBinary},
	}
	for _, c := range cases {
		result := captureBody(t, strings.NewReader(c.body), c.contentType, defaultLimits)
		if result.Request.Body.Kind != c.kind {
			t.Errorf("%q %q: kind = %s, want %s", c.contentType, c.body, result.Request.Body.Kind, c.kind)
		}
	}
}

func TestCharsets(t *testing.T) {
	latin1 := "a\xe7\xe3o" // "ação" em ISO-8859-1
	result := captureBody(t, strings.NewReader(latin1), "text/plain; charset=iso-8859-1", defaultLimits)
	if *result.Request.Body.Text != "ação" || *result.Request.Body.Charset != "iso-8859-1" {
		t.Fatalf("text = %q charset = %q", *result.Request.Body.Text, *result.Request.Body.Charset)
	}

	result = captureBody(t, strings.NewReader("\uFEFFolá"), `text/plain; charset="UTF-8"`, defaultLimits)
	if *result.Request.Body.Text != "olá" || *result.Request.Body.Charset != "utf-8" || len(result.Request.Problems) != 0 {
		t.Fatalf("BOM must be removed: %q %+v", *result.Request.Body.Text, result.Request.Problems)
	}

	result = captureBody(t, strings.NewReader("ol\xe1"), "text/plain; charset=utf-8", defaultLimits)
	if *result.Request.Body.Text != "ol\uFFFD" || hasProblem(result.Request.Problems, model.CharsetMismatch) == nil {
		t.Fatalf("text = %q problems = %v", *result.Request.Body.Text, codes(result.Request.Problems))
	}
}

func TestCutMultibyteCharacterIsStillText(t *testing.T) {
	// "ç" ocupa 2 bytes; o corte em 3 bytes deixa meio caractere no fim.
	result := captureBody(t, strings.NewReader("açaí"), "", Limits{MaxBodyBytes: 2, MaxRequestBytes: 100})
	if result.Request.Body.Kind != model.KindText || !result.Request.Body.Truncated {
		t.Fatalf("body = %+v", result.Request.Body)
	}
	if hasProblem(result.Request.Problems, model.CharsetMismatch) != nil {
		t.Fatal("a cut at the storage limit is not a charset problem")
	}
}

func TestFormFieldsShareTheBudget(t *testing.T) {
	result := captureBody(t, strings.NewReader("a=12345&b=67890&c=x"), "application/x-www-form-urlencoded", Limits{MaxBodyBytes: 7, MaxRequestBytes: 100})
	body := result.Request.Body
	if body.Kind != model.KindForm || !body.Truncated || body.Form["a"][0] != "12345" || body.Form["b"][0] != "67" || body.Form["c"][0] != "" {
		t.Fatalf("body = %+v", body)
	}
	if hasProblem(result.Request.Problems, model.BodyTruncated) == nil {
		t.Fatalf("problems = %v", codes(result.Request.Problems))
	}
}

func TestLenientURLEncoded(t *testing.T) {
	result := captureBody(t, strings.NewReader("a=%zz&b=1+2&&c"), "application/x-www-form-urlencoded", defaultLimits)
	form := result.Request.Body.Form
	if len(result.Request.Problems) != 0 || form["a"][0] != "%zz" || form["b"][0] != "1 2" || form["c"][0] != "" {
		t.Fatalf("form = %v problems = %v", form, codes(result.Request.Problems))
	}
}

func TestMultipartOnlyFilesHasEmptyForm(t *testing.T) {
	body := "--b\r\nContent-Disposition: form-data; name=\"f\"; filename=\"x.bin\"\r\n\r\nabc\r\n--b--\r\n"
	result := captureBody(t, strings.NewReader(body), "multipart/form-data; boundary=b", defaultLimits)
	b := result.Request.Body
	if b.Kind != model.KindMultipart || b.Form == nil || len(b.Form) != 0 || len(b.Files) != 1 || b.Files[0].Length != 3 || b.Files[0].ContentType != nil {
		t.Fatalf("body = %+v", b)
	}
}

func TestEmptyBody(t *testing.T) {
	result := captureBody(t, nil, "application/json", defaultLimits)
	if result.Request.Body.Kind != model.KindNone || len(result.Request.Problems) != 0 {
		t.Fatalf("body = %+v problems = %v", result.Request.Body, codes(result.Request.Problems))
	}
}
