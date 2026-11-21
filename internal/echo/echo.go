// Package echo é o catch-all: responde a qualquer método em qualquer path (exceto as rotas
// do painel, sob ECHO_DASHBOARD_PATH)
// devolvendo o que recebeu, em HTML para navegadores e JSON para o resto.
package echo

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luizcarlosfaria/echo-server/internal/capture"
	"github.com/luizcarlosfaria/echo-server/internal/config"
	"github.com/luizcarlosfaria/echo-server/internal/forwarded"
	"github.com/luizcarlosfaria/echo-server/internal/model"
	"github.com/luizcarlosfaria/echo-server/internal/store"
	"github.com/luizcarlosfaria/echo-server/internal/web"
)

// BodyIdleTimeout é quanto tempo a leitura do body espera por mais dados antes de desistir
// (um cliente parado não segura a conexão para sempre).
const BodyIdleTimeout = 30 * time.Second

// Handler é o catch-all do echo.
type Handler struct {
	options     config.Options
	store       *store.Store
	machineName string
}

// New cria o handler.
func New(options config.Options, st *store.Store) *Handler {
	machineName, _ := os.Hostname()
	return &Handler{options: options, store: st, machineName: machineName}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info := forwarded.FromRequest(r)
	rc := http.NewResponseController(w)

	result := capture.Capture(r, info, capture.Limits{
		MaxBodyBytes:    h.options.MaxBodyBytes,
		MaxRequestBytes: h.options.MaxRequestBytes,
	}, func() { _ = rc.SetReadDeadline(time.Now().Add(BodyIdleTimeout)) })
	_ = rc.SetReadDeadline(time.Time{})

	stored := h.store.Add(result.Request)

	// A conexão caiu no meio do body: o request fica no histórico (com o problema), mas não
	// há para quem responder. ErrAbortHandler fecha a conexão sem logar stack trace.
	if result.Broken || r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}

	header := w.Header()
	header.Set("X-Echo-Request-Id", strconv.FormatInt(stored.ID, 10))
	if codes := distinctCodes(stored.Problems); codes != "" {
		header.Set("X-Echo-Problems", codes)
	}

	if r.Method == http.MethodHead {
		// HEAD: mesmos headers do GET, sem corpo.
		header.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}

	if WantsHTML(r) {
		if err := web.RenderEcho(w, h.page(stored)); err != nil {
			slog.Error("failed to render the echo page", "error", err)
		}
		return
	}

	payload, err := MarshalEcho(h.Response(stored))
	if err != nil {
		slog.Error("failed to serialize the echo", "error", err)
		http.Error(w, "failed to serialize the echo", http.StatusInternalServerError)
		return
	}
	header.Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(payload)
}

func distinctCodes(problems []model.Problem) string {
	var codes []string
	seen := map[string]bool{}
	for _, p := range problems {
		if !seen[p.Code] {
			seen[p.Code] = true
			codes = append(codes, p.Code)
		}
	}
	return strings.Join(codes, ", ")
}

// WantsHTML: navegadores pedem text/html; curl, HttpClient e afins recebem JSON.
func WantsHTML(r *http.Request) bool {
	for _, line := range r.Header.Values("Accept") {
		for _, item := range strings.Split(line, ",") {
			mediaType, params, _ := strings.Cut(item, ";")
			mediaType = strings.ToLower(strings.TrimSpace(mediaType))
			if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
				continue
			}
			if quality(params) != 0 {
				return true
			}
		}
	}
	return false
}

// quality devolve o parâmetro q (1 quando ausente, 0 quando inválido).
func quality(params string) float64 {
	for _, param := range strings.Split(params, ";") {
		name, value, _ := strings.Cut(param, "=")
		if strings.EqualFold(strings.TrimSpace(name), "q") {
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || q < 0 || q > 1 {
				return 0
			}
			return q
		}
	}
	return 1
}

// ------------------------------------------------------------------ HTML

func (h *Handler) page(request *model.Request) web.EchoPage {
	page := web.EchoPage{
		Title:           h.options.AppName + " · Echo Server",
		AppName:         h.options.AppName,
		HeaderColor:     h.options.HeaderColor,
		Now:             time.Now().Format(localLayout),
		DashboardURL:    request.PathBase + h.options.DashboardPath,
		MachineName:     h.machineName,
		Request:         request,
		Headers:         keyValues(request.Headers, true),
		Query:           keyValues(request.Query, false),
		Form:            keyValues(request.Body.Form, false),
		ShowEnvironment: h.options.ShowEnvironment,
	}
	if h.options.ShowEnvironment {
		for _, kv := range sortedEnvironment() {
			page.Environment = append(page.Environment, web.KeyValue{Key: kv[0], Value: kv[1]})
		}
	}
	return page
}

func keyValues(source map[string][]string, ignoreCase bool) []web.KeyValue {
	keys := sortedKeys(source)
	if ignoreCase {
		sort.SliceStable(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
	}
	result := make([]web.KeyValue, 0, len(keys))
	for _, key := range keys {
		result = append(result, web.KeyValue{Key: key, Value: strings.Join(source[key], ", ")})
	}
	return result
}

func sortedKeys(source map[string][]string) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ------------------------------------------------------------------ JSON

// MarshalEcho serializa como o echo histórico: indentado com 2 espaços e sem escapar HTML.
func MarshalEcho(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Layouts de Now/UtcNow no JSON do echo: ISO 8601 com 7 casas decimais fixas.
const (
	localLayout = "2006-01-02T15:04:05.0000000-07:00"
	utcLayout   = "2006-01-02T15:04:05.0000000Z"
)

// Response é o JSON do echo. A ordem dos campos é o contrato histórico.
type Response struct {
	RequestID            int64               `json:"RequestId"`
	AppName              string              `json:"AppName"`
	Now                  string              `json:"Now"`
	UtcNow               string              `json:"UtcNow"`
	Headers              map[string][]string `json:"Headers"`
	Path                 string              `json:"Path"`
	PathBase             string              `json:"PathBase"`
	MachineName          string              `json:"MachineName"`
	Method               string              `json:"Method"`
	Scheme               string              `json:"Scheme"`
	Host                 string              `json:"Host"`
	Protocol             string              `json:"Protocol"`
	RemoteIP             *string             `json:"RemoteIp"`
	Problems             []Problem           `json:"Problems"`
	QueryString          []KeyValues         `json:"QueryString"`
	Forms                []KeyValues         `json:"Forms"`
	Payload              json.RawMessage     `json:"Payload,omitzero"`
	Body                 Body                `json:"Body"`
	EnvironmentVariables map[string]string   `json:"EnvironmentVariables,omitzero"`
}

// Problem em PascalCase.
type Problem struct {
	Code     string `json:"Code"`
	Severity string `json:"Severity"`
	Message  string `json:"Message"`
}

// KeyValues é o formato legado: { "Key": "a", "Value": ["1", "2"] }.
type KeyValues struct {
	Key   string   `json:"Key"`
	Value []string `json:"Value"`
}

// Body em PascalCase. O JSON parseado não é repetido aqui: ele já aparece em Payload.
type Body struct {
	Kind      string              `json:"Kind"`
	Size      int64               `json:"Size"`
	Truncated bool                `json:"Truncated"`
	Charset   *string             `json:"Charset,omitempty"`
	Text      *string             `json:"Text,omitempty"`
	JSONError *string             `json:"JsonError,omitempty"`
	Form      map[string][]string `json:"Form,omitzero"`
	Files     []File              `json:"Files,omitzero"`
	Base64    *string             `json:"Base64,omitempty"`
	Error     *string             `json:"Error,omitempty"`
}

// File em PascalCase.
type File struct {
	Name        string  `json:"Name"`
	FileName    string  `json:"FileName"`
	ContentType *string `json:"ContentType,omitempty"`
	Length      int64   `json:"Length"`
}

// Response monta o JSON do echo para um request armazenado.
func (h *Handler) Response(request *model.Request) Response {
	now := time.Now()
	body := request.Body
	response := Response{
		RequestID:   request.ID,
		AppName:     h.options.AppName,
		Now:         now.Format(localLayout),
		UtcNow:      now.UTC().Format(utcLayout),
		Headers:     request.Headers,
		Path:        request.Path,
		PathBase:    request.PathBase,
		MachineName: h.machineName,
		Method:      request.Method,
		Scheme:      request.Scheme,
		Host:        request.Host,
		Protocol:    request.Protocol,
		RemoteIP:    request.RemoteIP,
		Problems:    make([]Problem, 0, len(request.Problems)),
		QueryString: legacy(request.Query),
		Forms:       legacy(body.Form),
		Payload:     body.JSON,
		Body: Body{
			Kind:      body.Kind,
			Size:      body.Size,
			Truncated: body.Truncated,
			Charset:   body.Charset,
			Text:      body.Text,
			JSONError: body.JSONError,
			Form:      body.Form,
			Base64:    body.Base64,
			Error:     body.Error,
		},
	}
	for _, p := range request.Problems {
		response.Problems = append(response.Problems, Problem(p))
	}
	if body.Files != nil {
		response.Body.Files = make([]File, 0, len(body.Files))
		for _, f := range body.Files {
			response.Body.Files = append(response.Body.Files, File(f))
		}
	}
	if h.options.ShowEnvironment {
		response.EnvironmentVariables = map[string]string{}
		for _, kv := range sortedEnvironment() {
			response.EnvironmentVariables[kv[0]] = kv[1]
		}
	}
	return response
}

func legacy(source map[string][]string) []KeyValues {
	result := make([]KeyValues, 0, len(source))
	for _, key := range sortedKeys(source) {
		result = append(result, KeyValues{Key: key, Value: source[key]})
	}
	return result
}

// sortedEnvironment devolve as variáveis de ambiente em ordem ordinal.
func sortedEnvironment() [][2]string {
	var result [][2]string
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if key == "" {
			continue
		}
		result = append(result, [2]string{key, value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i][0] < result[j][0] })
	return result
}
