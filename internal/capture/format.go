package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/encoding/ianaindex"

	"github.com/luizcarlosfaria/echo-server/internal/model"
)

var textualApplicationSubtypes = map[string]bool{
	"javascript": true, "ecmascript": true, "x-javascript": true, "x-ndjson": true, "ndjson": true,
	"jsonl": true, "x-jsonlines": true, "yaml": true, "x-yaml": true, "toml": true, "graphql": true,
	"sql": true, "csv": true, "x-sh": true, "x-httpd-php": true, "rtf": true,
}

type mediaType struct {
	typ     string // "application"
	subtype string // "problem" (sem o sufixo)
	suffix  string // "json"
	params  map[string]string
	raw     string // "application/problem+json"
}

func parseMediaType(value string) (*mediaType, bool) {
	full, params, err := mime.ParseMediaType(value)
	if err == mime.ErrInvalidMediaParameter {
		err = nil // tipo legível com parâmetro malformado: mantém o tipo
	}
	if err != nil {
		return nil, false
	}
	typ, subtype, ok := strings.Cut(full, "/")
	if !ok || typ == "" || subtype == "" {
		return nil, false
	}
	mt := &mediaType{typ: typ, subtype: subtype, params: params, raw: full}
	if i := strings.LastIndex(subtype, "+"); i >= 0 {
		mt.subtype, mt.suffix = subtype[:i], subtype[i+1:]
	}
	if mt.params == nil {
		mt.params = map[string]string{}
	}
	return mt, true
}

func (m *mediaType) full() string { return m.raw }

func (m *mediaType) isForm() bool {
	return m.raw == "application/x-www-form-urlencoded" || m.raw == "multipart/form-data"
}

func classify(mt *mediaType, data []byte, incomplete bool) string {
	if mt == nil || mt.typ == "multipart" {
		return sniff(data, incomplete)
	}
	switch {
	case mt.subtype == "json" || mt.suffix == "json":
		return model.KindJSON
	case mt.subtype == "xml" || mt.suffix == "xml":
		return model.KindXML
	case mt.typ == "text" || mt.typ == "application" && textualApplicationSubtypes[mt.subtype]:
		return model.KindText
	default:
		return model.KindBinary
	}
}

// sniff decide entre json, texto e binário olhando os bytes (sem Content-Type útil).
func sniff(data []byte, incomplete bool) string {
	if !validUTF8AllowingCut(data, incomplete) || containsControlChars(data) {
		return model.KindBinary
	}
	first := bytes.TrimLeft(data, " \t\r\n")
	if !incomplete && len(first) > 0 && (first[0] == '{' || first[0] == '[') && json.Valid(data) {
		return model.KindJSON
	}
	return model.KindText
}

// validUTF8AllowingCut: um corte no meio de um caractere multibyte não transforma texto em binário.
func validUTF8AllowingCut(data []byte, incomplete bool) bool {
	if utf8.Valid(data) {
		return true
	}
	if !incomplete {
		return false
	}
	for cut := 1; cut <= 3 && cut < len(data); cut++ {
		if utf8.Valid(data[:len(data)-cut]) {
			return true
		}
	}
	return false
}

func containsControlChars(data []byte) bool {
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			return true
		}
	}
	return false
}

// resolveCharset devolve o nome do charset e a função que decodifica os bytes.
func (c *capturer) resolveCharset(mt *mediaType) (string, func([]byte) string) {
	if mt == nil || strings.TrimSpace(mt.params["charset"]) == "" {
		return "utf-8", decodeUTF8
	}
	charset := strings.Trim(strings.TrimSpace(mt.params["charset"]), `"`)

	enc, name := lookupEncoding(charset)
	if enc == nil {
		c.add(model.CharsetUnknown, fmt.Sprintf("Unknown charset '%s'; the body was decoded as UTF-8.", charset))
		return "utf-8", decodeUTF8
	}
	if name == "utf-8" {
		return name, decodeUTF8
	}
	return name, func(data []byte) string {
		decoded, err := enc.NewDecoder().Bytes(data)
		if err != nil {
			return decodeUTF8(data)
		}
		return string(decoded)
	}
}

// lookupEncoding procura primeiro nos nomes IANA (iso-8859-1 continua iso-8859-1) e depois
// nos rótulos WHATWG (utf8, latin1...). O nome devolvido é o preferido para MIME, como o
// WebName do .NET ("iso-8859-1", e não o canônico "ISO_8859-1:1987").
func lookupEncoding(charset string) (encoding.Encoding, string) {
	if enc, err := ianaindex.IANA.Encoding(charset); err == nil && enc != nil {
		for _, index := range []*ianaindex.Index{ianaindex.MIME, ianaindex.IANA} {
			if name, err := index.Name(enc); err == nil {
				return enc, strings.ToLower(name)
			}
		}
		return enc, strings.ToLower(charset)
	}
	if enc, err := htmlindex.Get(charset); err == nil && enc != nil {
		if name, err := htmlindex.Name(enc); err == nil {
			return enc, strings.ToLower(name)
		}
		return enc, strings.ToLower(charset)
	}
	return nil, ""
}

// decodeUTF8 troca cada byte inválido por U+FFFD, como o UTF8Encoding do .NET.
func decodeUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var sb strings.Builder
	sb.Grow(len(data) + 8)
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			sb.WriteRune(utf8.RuneError)
		} else {
			sb.Write(data[:size])
		}
		data = data[size:]
	}
	return sb.String()
}
