// Package config lê as variáveis de ambiente na subida do container.
// Valores inválidos impedem a subida (fail fast): o echo nunca assume o default em silêncio.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultAppName         = "Echo"
	DefaultHeaderColor     = "gray-900"
	DefaultMaxRequests     = 1000
	DefaultMaxBodyBytes    = 64 * 1024
	DefaultMaxRequestBytes = 30_000_000
	DefaultPort            = 80
)

// Options são as configurações do echo.
type Options struct {
	// AppName é o nome exibido no echo e no painel (APP_NAME).
	AppName string
	// HeaderColor é a cor Tailwind do cabeçalho (HEADER_COLOR).
	HeaderColor string
	// ShowEnvironment inclui as variáveis de ambiente no echo (ECHO_SHOW_ENVIRONMENT).
	ShowEnvironment bool
	// MaxRequests é a quantidade de requests mantidos em memória (ECHO_MAX_REQUESTS).
	MaxRequests int
	// MaxBodyBytes é a quantidade de bytes de body armazenados por request (ECHO_MAX_BODY_BYTES).
	MaxBodyBytes int
	// MaxRequestBytes é a quantidade de bytes de body lidos por request (ECHO_MAX_REQUEST_BYTES).
	// Acima disso o echo para de ler, mas responde 200 e sinaliza body_too_large.
	MaxRequestBytes int64
	// Port é a porta HTTP (ECHO_PORT).
	Port int
}

var (
	trueValues  = []string{"true", "1", "yes", "y", "on", "sim", "s"}
	falseValues = []string{"false", "0", "no", "n", "off", "nao", "não"}
)

// Load monta as opções a partir de getenv (normalmente os.Getenv).
// Todos os erros são devolvidos juntos, cada um citando a variável errada.
func Load(getenv func(string) string) (Options, error) {
	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	showEnvironment, err := readBool(getenv, "ECHO_SHOW_ENVIRONMENT", true)
	collect(err)
	maxRequests, err := readPositiveInt(getenv, "ECHO_MAX_REQUESTS", DefaultMaxRequests)
	collect(err)
	maxBodyBytes, err := readPositiveInt(getenv, "ECHO_MAX_BODY_BYTES", DefaultMaxBodyBytes)
	collect(err)
	maxRequestBytes, err := readPositiveInt(getenv, "ECHO_MAX_REQUEST_BYTES", DefaultMaxRequestBytes)
	collect(err)
	port, err := readPositiveInt(getenv, "ECHO_PORT", DefaultPort)
	collect(err)
	if err == nil && port > 65535 {
		collect(fmt.Errorf("the environment variable ECHO_PORT must be between 1 and 65535, but got '%d'", port))
	}

	if len(errs) > 0 {
		return Options{}, errors.Join(errs...)
	}

	return Options{
		AppName:         readString(getenv, "APP_NAME", DefaultAppName),
		HeaderColor:     readString(getenv, "HEADER_COLOR", DefaultHeaderColor),
		ShowEnvironment: showEnvironment,
		MaxRequests:     maxRequests,
		MaxBodyBytes:    maxBodyBytes,
		MaxRequestBytes: int64(maxRequestBytes),
		Port:            port,
	}, nil
}

func readString(getenv func(string) string, key, fallback string) string {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func readBool(getenv func(string) string, key string, fallback bool) (bool, error) {
	raw := getenv(key)
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	for _, candidate := range trueValues {
		if strings.EqualFold(value, candidate) {
			return true, nil
		}
	}
	for _, candidate := range falseValues {
		if strings.EqualFold(value, candidate) {
			return false, nil
		}
	}
	return false, fmt.Errorf("the environment variable %s must be true or false, but got '%s'", key, raw)
}

// readPositiveInt aceita inteiros de 32 bits maiores que zero (mesma faixa do int do .NET).
func readPositiveInt(getenv func(string) string, key string, fallback int) (int, error) {
	raw := getenv(key)
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("the environment variable %s must be a positive integer, but got '%s'", key, raw)
	}
	return int(parsed), nil
}
