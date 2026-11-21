// Package web embute as páginas HTML (html/template + Tailwind via CDN) e o front-end do painel.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"time"

	"github.com/luizcarlosfaria/echo-server/internal/model"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/dashboard.js
var dashboardJS []byte

//go:embed static/favicon.ico
var favicon []byte

var (
	dashboardJSVersion = func() string {
		sum := sha256.Sum256(dashboardJS)
		return hex.EncodeToString(sum[:])[:16]
	}()
	startedAt = time.Now()
)

var funcs = template.FuncMap{
	"badge": func(severity string) string {
		switch severity {
		case model.SeverityError:
			return "bg-red-100 text-red-800"
		case model.SeverityWarning:
			return "bg-amber-100 text-amber-800"
		default:
			return "bg-gray-100 text-gray-700"
		}
	},
}

var (
	echoPage      = page("echo.html")
	dashboardPage = page("dashboard.html")
)

func page(name string) *template.Template {
	return template.Must(template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+name))
}

// KeyValue é uma linha das tabelas de chave/valor.
type KeyValue struct {
	Key   string
	Value string
}

// EchoPage é o modelo da página HTML do echo.
type EchoPage struct {
	Title           string
	AppName         string
	HeaderColor     string
	Now             string
	DashboardURL    string
	MachineName     string
	Request         *model.Request
	Headers         []KeyValue
	Query           []KeyValue
	Form            []KeyValue
	ShowEnvironment bool
	Environment     []KeyValue
}

// DashboardPage é o modelo da página do painel.
type DashboardPage struct {
	Title       string
	AppName     string
	HeaderColor string
	Base        string
	Capacity    int
	Origin      string
	ScriptURL   string
}

// RenderEcho escreve a página do echo.
func RenderEcho(w http.ResponseWriter, data EchoPage) error { return render(w, echoPage, data) }

// RenderDashboard escreve a página do painel.
func RenderDashboard(w http.ResponseWriter, data DashboardPage) error {
	return render(w, dashboardPage, data)
}

// Renderiza num buffer antes: um erro de template não deixa uma página pela metade.
func render(w http.ResponseWriter, t *template.Template, data any) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := w.Write(buf.Bytes())
	return err
}

// DashboardScriptVersion é anexado à URL do script (?v=...) para invalidar o cache a cada versão.
func DashboardScriptVersion() string { return dashboardJSVersion }

// FaviconPath é servido como arquivo estático (como o wwwroot do .NET): o pedido automático
// do navegador não é ecoado nem entra no histórico.
const FaviconPath = "/favicon.ico"

// ServeFavicon entrega o ícone.
func ServeFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	http.ServeContent(w, r, "favicon.ico", startedAt, bytes.NewReader(favicon))
}

// ServeDashboardScript entrega o dashboard.js (com ETag e Last-Modified).
func ServeDashboardScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("ETag", `"`+dashboardJSVersion+`"`)
	http.ServeContent(w, r, "dashboard.js", startedAt, bytes.NewReader(dashboardJS))
}
