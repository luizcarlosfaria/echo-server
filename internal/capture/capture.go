// Package capture converte um *http.Request em model.Request.
// Nunca rejeita o request: falhas de leitura, formato ou codificação viram model.Problem
// e o que foi possível ler é preservado.
package capture

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luizcarlosfaria/echo-server/internal/forwarded"
	"github.com/luizcarlosfaria/echo-server/internal/model"
)

// Limits são os limites de body (ECHO_MAX_BODY_BYTES e ECHO_MAX_REQUEST_BYTES).
type Limits struct {
	MaxBodyBytes    int
	MaxRequestBytes int64
}

// Result é o request capturado.
type Result struct {
	Request model.Request
	// Broken indica que a conexão falhou durante a leitura do body: não há como responder.
	Broken bool
}

// Capture lê o request inteiro. touch é chamado antes de cada leitura do body (para renovar
// o deadline de leitura da conexão) e pode ser nil.
func Capture(r *http.Request, info forwarded.Info, limits Limits, touch func()) Result {
	c := &capturer{limits: limits, touch: touch}

	rawContentType := strings.Join(r.Header.Values("Content-Type"), ", ")
	var mt *mediaType
	if strings.TrimSpace(rawContentType) != "" {
		if parsed, ok := parseMediaType(rawContentType); ok {
			mt = parsed
		} else {
			c.add(model.ContentTypeInvalid, fmt.Sprintf("Content-Type '%s' could not be parsed; the body format was inferred from its content.", rawContentType))
		}
	}

	body, broken := c.captureBody(r, mt)

	request := model.Request{
		Timestamp:   model.Timestamp(time.Now()),
		Method:      r.Method,
		Scheme:      info.Scheme,
		Host:        info.Host,
		PathBase:    info.PathBase,
		Path:        r.URL.Path,
		QueryString: "",
		Query:       map[string][]string(r.URL.Query()),
		Headers:     map[string][]string(r.Header),
		Protocol:    r.Proto,
		Body:        body,
		Problems:    c.problems,
	}
	if request.Path == "" {
		request.Path = "/"
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		request.QueryString = "?" + r.URL.RawQuery
	}
	if request.Headers == nil {
		request.Headers = map[string][]string{}
	}
	if info.RemoteIP != "" {
		request.RemoteIP = model.Ptr(info.RemoteIP)
	}
	if rawContentType != "" || len(r.Header.Values("Content-Type")) > 0 {
		request.ContentType = model.Ptr(rawContentType)
	}
	if declared, ok := declaredLength(r); ok {
		request.ContentLength = model.Ptr(declared)
	}
	if request.Problems == nil {
		request.Problems = []model.Problem{}
	}
	return Result{Request: request, Broken: broken}
}

var (
	byteOrderMark   = string(rune(0xFEFF))
	replacementChar = string(utf8.RuneError)
)

type capturer struct {
	limits   Limits
	touch    func()
	problems []model.Problem
}

func (c *capturer) add(code, message string) {
	c.problems = append(c.problems, model.NewProblem(code, message))
}

func declaredLength(r *http.Request) (int64, bool) {
	raw := r.Header.Get("Content-Length")
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return value, err == nil
}

// ------------------------------------------------------------------ body

func (c *capturer) captureBody(r *http.Request, mt *mediaType) (model.Body, bool) {
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	reader := newLimitedReader(body, c.limits.MaxBodyBytes, c.limits.MaxRequestBytes, c.touch)

	if mt == nil || !mt.isForm() {
		reader.drain()
		c.reportOutcome(r, reader)
		return c.interpret(reader, mt), reader.failure != nil
	}

	// Form/multipart: o parser consome o stream enquanto o limitedReader guarda os primeiros
	// MaxBodyBytes brutos. Se algo der errado, o conteúdo bruto é devolvido no lugar do form.
	var fields map[string][]string
	var files []model.File
	var truncated bool
	var parseErr error
	if mt.full() == "multipart/form-data" {
		fields, files, truncated, parseErr = c.parseMultipart(reader, mt.params["boundary"])
		if files == nil {
			files = []model.File{}
		}
	} else {
		data, _ := io.ReadAll(reader)
		fields, truncated = c.parseURLEncoded(data)
	}
	reader.drain()
	c.reportOutcome(r, reader)

	// Body incompleto/excessivo: o form não pode ser confiado.
	if reader.incomplete() {
		return c.interpret(reader, mt), reader.failure != nil
	}

	if parseErr != nil {
		message := "Invalid form: " + parseErr.Error()
		c.add(model.FormInvalid, message)
		raw := c.interpret(reader, mt)
		raw.Error = model.Ptr(message)
		return raw, false
	}

	if truncated {
		c.add(model.BodyTruncated, c.truncationMessage(reader.total))
	}
	result := model.Body{Kind: model.KindForm, Size: reader.total, Truncated: truncated, Form: fields}
	if mt.full() == "multipart/form-data" {
		result.Kind = model.KindMultipart
		result.Files = files
	}
	return result, false
}

// reportOutcome converte o resultado da leitura do stream em problemas.
func (c *capturer) reportOutcome(r *http.Request, reader *limitedReader) {
	if reader.failure != nil {
		c.problems = append(c.problems, *reader.failure)
	}
	if reader.limitReached {
		c.add(model.BodyTooLarge, fmt.Sprintf("The body exceeds ECHO_MAX_REQUEST_BYTES (%d bytes); reading stopped and the rest was discarded.", c.limits.MaxRequestBytes))
	} else if declared, ok := declaredLength(r); ok && reader.failure == nil && declared != reader.total {
		c.add(model.ContentLengthMismatch, fmt.Sprintf("Content-Length declares %d bytes, but %d were received.", declared, reader.total))
	}
}

func (c *capturer) truncationMessage(total int64) string {
	return fmt.Sprintf("Body of %d bytes partially stored (ECHO_MAX_BODY_BYTES = %d).", total, c.limits.MaxBodyBytes)
}

// interpret transforma os bytes guardados em json, xml, texto ou binário.
func (c *capturer) interpret(reader *limitedReader, mt *mediaType) model.Body {
	if reader.total == 0 {
		return model.Body{Kind: model.KindNone}
	}

	data := reader.kept
	truncated := reader.total > int64(len(data))
	if truncated {
		c.add(model.BodyTruncated, c.truncationMessage(reader.total))
	}

	incomplete := truncated || reader.incomplete()
	kind := classify(mt, data, incomplete)
	body := model.Body{Kind: kind, Size: reader.total, Truncated: truncated}

	if kind == model.KindBinary {
		body.Base64 = model.Ptr(base64.StdEncoding.EncodeToString(data))
		return body
	}

	charset, decode := c.resolveCharset(mt)
	if charset == "utf-8" && !validUTF8AllowingCut(data, incomplete) {
		c.add(model.CharsetMismatch, "The body contains invalid UTF-8 bytes; invalid characters were replaced with '"+replacementChar+"'. Check the declared charset.")
	}
	text := strings.TrimLeft(decode(data), byteOrderMark)
	body.Charset = model.Ptr(charset)
	body.Text = model.Ptr(text)

	if kind != model.KindJSON {
		return body
	}
	if incomplete {
		body.JSONError = model.Ptr(fmt.Sprintf("Body incomplete or truncated (%d of %d bytes stored); JSON was not parsed.", len(data), reader.total))
		return body
	}
	if json.Valid([]byte(text)) {
		body.JSON = json.RawMessage(text)
		return body
	}
	var discard any
	err := json.Unmarshal([]byte(text), &discard)
	message := "invalid JSON"
	if err != nil {
		message = err.Error()
	}
	c.add(model.JSONInvalid, "Invalid JSON: "+message)
	body.JSONError = model.Ptr(message)
	return body
}

// ------------------------------------------------------------------ forms

// parseURLEncoded é tolerante como o FormReader do ASP.NET: escapes inválidos ficam literais.
func (c *capturer) parseURLEncoded(data []byte) (map[string][]string, bool) {
	fields := map[string][]string{}
	budget := c.limits.MaxBodyBytes
	truncated := false
	for _, pair := range strings.Split(string(data), "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key, value = unescapeForm(key), unescapeForm(value)
		if key == "" && value == "" {
			continue
		}
		value, cut := cutToBudget(value, &budget)
		truncated = truncated || cut
		fields[key] = append(fields[key], value)
	}
	return fields, truncated
}

func unescapeForm(value string) string {
	if unescaped, err := url.QueryUnescape(value); err == nil {
		return unescaped
	}
	return strings.ReplaceAll(value, "+", " ")
}

func (c *capturer) parseMultipart(reader io.Reader, boundary string) (map[string][]string, []model.File, bool, error) {
	if boundary == "" {
		return nil, nil, false, fmt.Errorf("Missing content-type boundary.")
	}

	fields := map[string][]string{}
	files := []model.File{}
	budget := c.limits.MaxBodyBytes
	truncated := false

	parts := multipart.NewReader(reader, boundary)
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			return fields, files, truncated, nil
		}
		if err != nil {
			return nil, nil, false, err
		}

		name, fileName, isFile := contentDisposition(part)
		if isFile {
			length, err := io.Copy(io.Discard, part)
			if err != nil {
				return nil, nil, false, err
			}
			file := model.File{Name: name, FileName: fileName, Length: length}
			if contentType := part.Header.Get("Content-Type"); contentType != "" {
				file.ContentType = model.Ptr(contentType)
			}
			files = append(files, file)
			continue
		}

		// Guarda só o que cabe no orçamento; o resto do campo é descartado (mas contado).
		value, err := io.ReadAll(io.LimitReader(part, int64(budget)+utf8.UTFMax))
		if err != nil {
			return nil, nil, false, err
		}
		rest, err := io.Copy(io.Discard, part)
		if err != nil {
			return nil, nil, false, err
		}
		text, cut := cutToBudget(string(value), &budget)
		truncated = truncated || cut || rest > 0
		fields[name] = append(fields[name], text)
	}
}

func contentDisposition(part *multipart.Part) (name, fileName string, isFile bool) {
	_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil {
		return part.FormName(), "", false
	}
	fileName, isFile = params["filename"]
	return params["name"], fileName, isFile
}

// cutToBudget corta value para caber no orçamento restante (sem quebrar caracteres).
func cutToBudget(value string, budget *int) (string, bool) {
	cut := false
	if len(value) > *budget {
		end := max(*budget, 0)
		for end > 0 && !utf8.RuneStart(value[end]) {
			end--
		}
		value = value[:end]
		cut = true
	}
	*budget -= len(value)
	return value, cut
}
