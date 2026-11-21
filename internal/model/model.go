// Package model define a fotografia de um request recebido: o que o echo devolve,
// o que fica no histórico em memória e o que o painel exibe.
//
// As tags JSON (camelCase) são o contrato da API /_echo/api e do WebSocket.
// O echo usa PascalCase e é montado à parte, no pacote echo.
package model

import (
	"encoding/json"
	"time"
)

// Tipos de body (Body.Kind).
const (
	KindNone      = "none"
	KindJSON      = "json"
	KindXML       = "xml"
	KindText      = "text"
	KindForm      = "form"
	KindMultipart = "multipart"
	KindBinary    = "binary"
)

// Severidades de um problema.
const (
	// SeverityError: o request está malformado ou chegou incompleto.
	SeverityError = "error"
	// SeverityWarning: o request foi interpretado, mas com suposições (ex.: charset desconhecido).
	SeverityWarning = "warning"
	// SeverityInfo: limitação do próprio echo ao armazenar o request (ex.: body truncado).
	SeverityInfo = "info"
)

// Códigos de problema.
const (
	JSONInvalid           = "json_invalid"
	FormInvalid           = "form_invalid"
	BodyIncomplete        = "body_incomplete"
	ClientAborted         = "client_aborted"
	ContentLengthMismatch = "content_length_mismatch"
	BodyTooLarge          = "body_too_large"
	ContentTypeInvalid    = "content_type_invalid"
	CharsetUnknown        = "charset_unknown"
	CharsetMismatch       = "charset_mismatch"
	BodyTruncated         = "body_truncated"
)

// Problem é algo de errado (ou digno de nota) no request. O echo nunca rejeita o request
// por isso: responde normalmente e sinaliza o problema.
type Problem struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// NewProblem cria um problema com a severidade associada ao código.
func NewProblem(code, message string) Problem {
	return Problem{Code: code, Severity: SeverityOf(code), Message: message}
}

// SeverityOf devolve a severidade de um código de problema.
func SeverityOf(code string) string {
	switch code {
	case JSONInvalid, FormInvalid, BodyIncomplete, ClientAborted, ContentLengthMismatch:
		return SeverityError
	case BodyTooLarge, ContentTypeInvalid, CharsetUnknown, CharsetMismatch:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}

// File são os metadados de um arquivo de multipart (o conteúdo não é armazenado).
type File struct {
	Name        string  `json:"name"`
	FileName    string  `json:"fileName"`
	ContentType *string `json:"contentType,omitempty"`
	Length      int64   `json:"length"`
}

// Body é o que foi capturado do body do request.
type Body struct {
	// Kind é um dos Kind*.
	Kind string `json:"kind"`
	// Size é o tamanho real do body em bytes (mesmo quando truncado).
	Size int64 `json:"size"`
	// Truncated indica que o body excedeu ECHO_MAX_BODY_BYTES e só parte foi armazenada.
	Truncated bool `json:"truncated"`
	// Charset usado para decodificar o texto.
	Charset *string `json:"charset,omitempty"`
	// Text é o conteúdo textual (json, xml, texto ou fallback de form inválido).
	Text *string `json:"text,omitempty"`
	// JSON é o JSON parseado, quando válido e completo.
	JSON json.RawMessage `json:"json,omitzero"`
	// JSONError é o motivo pelo qual o JSON não pôde ser parseado.
	JSONError *string `json:"jsonError,omitempty"`
	// Form são os campos de formulário (form-urlencoded ou multipart).
	Form map[string][]string `json:"form,omitzero"`
	// Files são os metadados dos arquivos de um multipart.
	Files []File `json:"files,omitzero"`
	// Base64 é o conteúdo binário (possivelmente truncado).
	Base64 *string `json:"base64,omitempty"`
	// Error é o erro ao interpretar o body (ex.: multipart malformado).
	Error *string `json:"error,omitempty"`
}

// Request é a fotografia imutável de um request recebido.
type Request struct {
	ID            int64               `json:"id"`
	Timestamp     Timestamp           `json:"timestamp"`
	Method        string              `json:"method"`
	Scheme        string              `json:"scheme"`
	Host          string              `json:"host"`
	PathBase      string              `json:"pathBase"`
	Path          string              `json:"path"`
	QueryString   string              `json:"queryString"`
	Query         map[string][]string `json:"query"`
	Headers       map[string][]string `json:"headers"`
	RemoteIP      *string             `json:"remoteIp,omitempty"`
	Protocol      string              `json:"protocol"`
	ContentType   *string             `json:"contentType,omitempty"`
	ContentLength *int64              `json:"contentLength,omitempty"`
	Body          Body                `json:"body"`
	Problems      []Problem           `json:"problems"`
}

// Summary é a versão enxuta (sem body/headers) usada na listagem e no WebSocket.
type Summary struct {
	ID              int64     `json:"id"`
	Timestamp       Timestamp `json:"timestamp"`
	Method          string    `json:"method"`
	Scheme          string    `json:"scheme"`
	Host            string    `json:"host"`
	Path            string    `json:"path"`
	QueryString     string    `json:"queryString"`
	RemoteIP        *string   `json:"remoteIp,omitempty"`
	ContentType     *string   `json:"contentType,omitempty"`
	BodyKind        string    `json:"bodyKind"`
	BodySize        int64     `json:"bodySize"`
	BodyTruncated   bool      `json:"bodyTruncated"`
	Problems        []string  `json:"problems"`
	ProblemSeverity *string   `json:"problemSeverity,omitempty"`
}

// Summary devolve o resumo do request.
func (r *Request) Summary() Summary {
	codes := make([]string, 0, len(r.Problems))
	for _, p := range r.Problems {
		codes = append(codes, p.Code)
	}
	return Summary{
		ID:              r.ID,
		Timestamp:       r.Timestamp,
		Method:          r.Method,
		Scheme:          r.Scheme,
		Host:            r.Host,
		Path:            r.Path,
		QueryString:     r.QueryString,
		RemoteIP:        r.RemoteIP,
		ContentType:     r.ContentType,
		BodyKind:        r.Body.Kind,
		BodySize:        r.Body.Size,
		BodyTruncated:   r.Body.Truncated,
		Problems:        codes,
		ProblemSeverity: worstSeverity(r.Problems),
	}
}

func worstSeverity(problems []Problem) *string {
	if len(problems) == 0 {
		return nil
	}
	worst := SeverityInfo
	for _, p := range problems {
		switch p.Severity {
		case SeverityError:
			worst = SeverityError
		case SeverityWarning:
			if worst != SeverityError {
				worst = SeverityWarning
			}
		}
	}
	return &worst
}

// Timestamp serializa em ISO 8601:
// UTC com offset explícito (+00:00) e até 7 casas decimais, sem zeros à direita.
type Timestamp time.Time

const timestampLayout = "2006-01-02T15:04:05.9999999-07:00"

// MarshalJSON implementa json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(t).UTC().Format(timestampLayout) + `"`), nil
}

// Time devolve o time.Time.
func (t Timestamp) Time() time.Time { return time.Time(t) }

// Ptr devolve um ponteiro para v (campos opcionais).
func Ptr[T any](v T) *T { return &v }
