FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# Binário estático; timetzdata embute o banco de fusos (TZ funciona mesmo sem /usr/share/zoneinfo).
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata -ldflags="-s -w" -o /out/echo-server ./cmd/echo-server

FROM gcr.io/distroless/static-debian12 AS final
ENV TZ=America/Sao_Paulo \
    # Porta HTTP; 80 por compatibilidade (docker run -p 81:80).
    ECHO_PORT=80 \
    # Quantidade de requests mantidos em memória para o painel /_echo (expurga os mais antigos).
    ECHO_MAX_REQUESTS=1000 \
    # Bytes de body armazenados por request (o excedente é truncado; o echo informa o tamanho real).
    ECHO_MAX_BODY_BYTES=65536 \
    # Bytes lidos por body; acima disso o echo responde 200 e sinaliza body_too_large (em vez de 413).
    ECHO_MAX_REQUEST_BYTES=30000000 \
    # Exibe as variáveis de ambiente no echo; use false se elas puderem conter segredos.
    ECHO_SHOW_ENVIRONMENT=true
COPY --from=build /out/echo-server /echo-server
EXPOSE 80
ENTRYPOINT ["/echo-server"]
