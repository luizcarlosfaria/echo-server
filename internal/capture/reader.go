package capture

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/luizcarlosfaria/echo-server/internal/model"
)

// limitedReader lê o body (inclusive chunked) contando tudo que chega, guardando no máximo
// keep bytes e parando após max bytes. Falhas de conexão não propagam como pânico: o que
// foi lido fica disponível junto com o problema.
//
// Ao atingir o limite ou falhar, passa a devolver io.EOF para que parsers (form, multipart)
// terminem; quem chama consulta limitReached/failure antes de confiar no resultado.
type limitedReader struct {
	r     io.Reader
	keep  int
	max   int64
	touch func()

	kept         []byte
	total        int64
	limitReached bool
	failure      *model.Problem
}

func newLimitedReader(r io.Reader, keep int, max int64, touch func()) *limitedReader {
	if touch == nil {
		touch = func() {}
	}
	return &limitedReader{r: r, keep: keep, max: max, touch: touch}
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.limitReached || l.failure != nil {
		return 0, io.EOF
	}
	// Lê até 1 byte além do limite para saber se ele foi realmente excedido.
	if allowed := l.max + 1 - l.total; int64(len(p)) > allowed {
		p = p[:allowed]
	}

	l.touch()
	n, err := l.r.Read(p)

	if l.total+int64(n) > l.max {
		n = int(l.max - l.total)
		l.limitReached = true
	}
	if room := l.keep - len(l.kept); room > 0 {
		l.kept = append(l.kept, p[:min(n, room)]...)
	}
	l.total += int64(n)

	switch {
	case l.limitReached:
		return n, io.EOF
	case err == nil || err == io.EOF:
		return n, err
	default:
		problem := readFailure(err, l.total)
		l.failure = &problem
		return n, io.EOF
	}
}

// drain lê o que sobrou do body.
func (l *limitedReader) drain() {
	_, _ = io.Copy(io.Discard, l)
}

// incomplete indica que o body não foi lido até o fim.
func (l *limitedReader) incomplete() bool { return l.limitReached || l.failure != nil }

// readFailure distingue a queda da conexão (client_aborted) de um body que terminou
// antes do esperado ou veio malformado (body_incomplete).
func readFailure(err error, total int64) model.Problem {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return model.NewProblem(model.BodyIncomplete, fmt.Sprintf("Body ended unexpectedly after %d bytes: timed out waiting for more data.", total))
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed) {
		return model.NewProblem(model.ClientAborted, fmt.Sprintf("The client closed the connection after sending %d bytes of the body.", total))
	}
	// io.ErrUnexpectedEOF (Content-Length maior que o enviado), chunked malformado...
	return model.NewProblem(model.BodyIncomplete, fmt.Sprintf("Body ended unexpectedly after %d bytes: %v", total, err))
}
