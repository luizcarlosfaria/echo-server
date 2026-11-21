// Package app monta o handler HTTP completo: X-Forwarded-*, rotas reservadas do painel
// (ECHO_DASHBOARD_PATH) e o echo.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/luizcarlosfaria/echo-server/internal/config"
	"github.com/luizcarlosfaria/echo-server/internal/dashboard"
	"github.com/luizcarlosfaria/echo-server/internal/echo"
	"github.com/luizcarlosfaria/echo-server/internal/forwarded"
	"github.com/luizcarlosfaria/echo-server/internal/store"
	"github.com/luizcarlosfaria/echo-server/internal/web"
)

// New devolve o handler da aplicação. stopping é cancelado no shutdown.
func New(options config.Options, st *store.Store, stopping context.Context) http.Handler {
	echoHandler := echo.New(options, st)
	dashboardHandler := dashboard.New(options, st, stopping)

	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dashboardHandler.IsReserved(r.URL.Path) {
			dashboardHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == web.FaviconPath && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			web.ServeFavicon(w, r)
			return
		}
		echoHandler.ServeHTTP(w, r)
	})

	// Primeiro o forwarded: tudo que vem depois enxerga Scheme/Host/IP originais.
	return recoverer(forwarded.Middleware(router))
}

// recoverer transforma um pânico em 500 sem stack trace para o cliente (fica só no log).
// http.ErrAbortHandler é repassado: é o jeito de fechar a conexão sem resposta.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			slog.Error("unhandled error", "method", r.Method, "path", r.URL.Path,
				"error", fmt.Sprint(v), "stack", string(debug.Stack()))
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"type":"https://tools.ietf.org/html/rfc9110#section-15.6.1","title":"An error occurred while processing your request.","status":500}`))
		}()
		next.ServeHTTP(w, r)
	})
}
