// Package forwarded aplica X-Forwarded-For/Proto/Host/Prefix de qualquer origem (todo proxy é
// confiável) consumindo só a entrada mais à direita de cada header: o proxy mais próximo.
// Os valores substituídos ficam visíveis em X-Original-For/Proto/Host/Prefix.
//
// O middleware também devolve a r.Header o que o net/http guarda fora dela (Host e
// Transfer-Encoding), para que o echo mostre os headers como chegaram.
package forwarded

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	forHeader    = "X-Forwarded-For"
	protoHeader  = "X-Forwarded-Proto"
	hostHeader   = "X-Forwarded-Host"
	prefixHeader = "X-Forwarded-Prefix"

	originalFor    = "X-Original-For"
	originalProto  = "X-Original-Proto"
	originalHost   = "X-Original-Host"
	originalPrefix = "X-Original-Prefix"
)

// Info é como o request chegou, já com X-Forwarded-* aplicado.
type Info struct {
	Scheme   string
	Host     string
	PathBase string
	// RemoteIP é vazio quando desconhecido.
	RemoteIP string
}

type contextKey struct{}

// FromRequest devolve o Info calculado pelo Middleware (ou o calculado na hora, sem o middleware).
func FromRequest(r *http.Request) Info {
	if info, ok := r.Context().Value(contextKey{}).(Info); ok {
		return info
	}
	return original(r)
}

// Middleware aplica os headers e repassa um request com Host, Header e Info atualizados.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Clone()
		if header == nil {
			header = http.Header{}
		}
		if r.Host != "" {
			header["Host"] = []string{r.Host}
		}
		if len(r.TransferEncoding) > 0 {
			header["Transfer-Encoding"] = []string{strings.Join(r.TransferEncoding, ", ")}
		}

		info := Apply(header, original(r), r.RemoteAddr)

		r2 := r.WithContext(context.WithValue(r.Context(), contextKey{}, info))
		r2.Header = header
		r2.Host = info.Host
		next.ServeHTTP(w, r2)
	})
}

func original(r *http.Request) Info {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	ip, _ := splitRemote(r.RemoteAddr)
	return Info{Scheme: scheme, Host: r.Host, RemoteIP: ip}
}

// Apply consome a entrada mais à direita de cada X-Forwarded-*, alterando header.
// Qualquer entrada inválida cancela tudo (para no primeiro erro, sem aplicar nada).
func Apply(header http.Header, current Info, remoteAddr string) Info {
	forValues := splitValues(header.Values(forHeader))
	protoValues := splitValues(header.Values(protoHeader))
	hostValues := splitValues(header.Values(hostHeader))
	prefixValues := splitValues(header.Values(prefixHeader))

	next := current
	var applyFor, applyProto, applyHost, applyPrefix bool

	if n := len(forValues); n > 0 {
		ip, ok := parseForwardedFor(forValues[n-1])
		if !ok {
			return current
		}
		next.RemoteIP = ip
		applyFor = true
	}
	if n := len(protoValues); n > 0 {
		if !validScheme(protoValues[n-1]) {
			return current
		}
		next.Scheme = protoValues[n-1]
		applyProto = true
	}
	if n := len(hostValues); n > 0 {
		if !validHost(hostValues[n-1]) {
			return current
		}
		next.Host = hostValues[n-1]
		applyHost = true
	}
	if n := len(prefixValues); n > 0 {
		prefix, ok := parsePrefix(prefixValues[n-1])
		if !ok {
			return current
		}
		next.PathBase = prefix
		applyPrefix = true
	}

	if applyFor {
		if ip, port := splitRemote(remoteAddr); ip != "" {
			header.Set(originalFor, net.JoinHostPort(ip, port))
		}
		consume(header, forHeader, forValues)
	}
	if applyProto {
		header.Set(originalProto, current.Scheme)
		consume(header, protoHeader, protoValues)
	}
	if applyHost {
		header.Set(originalHost, current.Host)
		header["Host"] = []string{next.Host}
		consume(header, hostHeader, hostValues)
	}
	if applyPrefix {
		// Um PathBase original vazio não gera X-Original-Prefix.
		if current.PathBase != "" {
			header.Set(originalPrefix, current.PathBase)
		}
		consume(header, prefixHeader, prefixValues)
	}
	return next
}

// consume remove a entrada consumida; as que sobram (proxies mais distantes) continuam no header.
func consume(header http.Header, name string, values []string) {
	if len(values) <= 1 {
		header.Del(name)
		return
	}
	header.Set(name, strings.Join(values[:len(values)-1], ", "))
}

func splitValues(lines []string) []string {
	var values []string
	for _, line := range lines {
		for _, part := range strings.Split(line, ",") {
			if part = strings.TrimSpace(part); part != "" {
				values = append(values, part)
			}
		}
	}
	return values
}

// splitRemote separa ip e porta de RemoteAddr, normalizando IPv4 mapeado em IPv6.
func splitRemote(remoteAddr string) (ip, port string) {
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "", ""
	}
	return addr.Unmap().String(), port
}

// parseForwardedFor aceita "ip", "ip:porta", "[ipv6]" e "[ipv6]:porta".
func parseForwardedFor(value string) (string, bool) {
	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr().Unmap().String(), true
	}
	if addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")); err == nil {
		return addr.Unmap().String(), true
	}
	return "", false
}

func validScheme(value string) bool {
	for i, c := range value {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return value != ""
}

// validHost aceita host[:porta] e [ipv6][:porta].
func validHost(value string) bool {
	if value == "" {
		return false
	}
	host := value
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end < 0 {
			return false
		}
		if _, err := netip.ParseAddr(value[1:end]); err != nil {
			return false
		}
		host, value = "", value[end+1:]
		if value != "" && !strings.HasPrefix(value, ":") {
			return false
		}
	} else if i := strings.LastIndex(value, ":"); i >= 0 {
		host, value = value[:i], value[i:]
	} else {
		value = ""
	}
	if value != "" {
		if _, err := strconv.ParseUint(value[1:], 10, 16); err != nil {
			return false
		}
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~%!$&'()*+,;=", c) || c > 127) {
			return false
		}
	}
	return true
}

func parsePrefix(value string) (string, bool) {
	if !strings.HasPrefix(value, "/") {
		return "", false
	}
	unescaped, err := url.PathUnescape(value)
	if err != nil {
		return "", false
	}
	return unescaped, true
}
