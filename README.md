# echo-server

Echo Server: serviço de teste para **proxy reverso, ingress controller e API gateway**.

Responde a **qualquer verbo em qualquer path** devolvendo exatamente o que recebeu: método, path, headers, query string, body, IP de origem e variáveis de ambiente do container. Também guarda os últimos requests em memória e os exibe **em tempo real** num painel web.

Antes de subir sua aplicação atrás de um proxy, valide o caminho com o echo.

| Echo 1  | Echo 2 |
| ------------- | ------------- |
| ![Echo 1](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Echo-Server1.png)  | ![Echo 2](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Echo-Server2.png) |
| `HEADER_COLOR=blue-600`  | `HEADER_COLOR=red-600`  |
| `APP_NAME=Echo 1`  |  `APP_NAME=Echo 2` |

## Sumário

- [Recursos](#recursos)
- [Início rápido](#início-rápido)
- [Variáveis de ambiente](#variáveis-de-ambiente)
- [Usando o echo](#usando-o-echo)
- [Formatos de body suportados](#formatos-de-body-suportados)
- [Requests com problemas](#requests-com-problemas)
- [Painel em tempo real](#painel-em-tempo-real)
- [API do histórico e WebSocket](#api-do-histórico-e-websocket)
- [Atrás de um proxy reverso](#atrás-de-um-proxy-reverso)
- [Desenvolvimento](#desenvolvimento)
- [Limitações conhecidas](#limitações-conhecidas)
- [Paleta de cores](#paleta-de-cores)

## Recursos

- **Echo completo** de qualquer verbo (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS…) em qualquer path.
- **HTML para navegadores, JSON para o resto**: a escolha é feita pelo header `Accept`.
- **Principais formatos de body**: JSON, XML, texto com charset, form-urlencoded, multipart (com arquivos) e binário.
- **Tolerante a falhas**: requests malformados são aceitos e os problemas são sinalizados, em vez de virar erro 500 ou 413.
- **Histórico em memória** dos últimos N requests (padrão 1000), descartando os mais antigos.
- **Painel em tempo real** (`/_echo`) via WebSocket, com filtro, pausa e navegação por teclado.
- **Pronto para proxies**: `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host` e `X-Forwarded-Prefix` são aplicados, e os valores originais ficam visíveis.
- **.NET 11**, imagem Linux escutando na **porta 80**.

## Início rápido

```sh
docker run -d -p 81:80 \
  -e HEADER_COLOR=blue-600 \
  -e APP_NAME="Echo 1" \
  luizcarlosfaria/echo-server:latest
```

- Echo no navegador: http://localhost:81
- Painel em tempo real: http://localhost:81/_echo

Para ver duas instâncias lado a lado, por exemplo atrás de um balanceador:

```sh
docker run -d -p 82:80 \
  -e HEADER_COLOR=red-600 \
  -e APP_NAME="Echo 2" \
  luizcarlosfaria/echo-server:latest
```

## Variáveis de ambiente

Todas são opcionais.

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `APP_NAME` | `Echo` | Nome exibido no echo e no painel. Útil para distinguir instâncias. |
| `HEADER_COLOR` | `gray-900` | Cor [Tailwind](#paleta-de-cores) do cabeçalho (ex.: `blue-600`, `red-600`). |
| `ECHO_SHOW_ENVIRONMENT` | `true` | Inclui as variáveis de ambiente do container no echo (JSON e HTML). Use `false` quando elas puderem conter segredos. |
| `ECHO_MAX_REQUESTS` | `1000` | Quantidade de requests mantidos no histórico em memória. Ao atingir o limite, os mais antigos são descartados. |
| `ECHO_MAX_BODY_BYTES` | `65536` | Bytes de body **armazenados** por request. O excedente é truncado (`Truncated: true`), mas `Size` informa o tamanho real. |
| `ECHO_MAX_REQUEST_BYTES` | `30000000` | Bytes **lidos** de cada body. Acima disso o echo para de ler, mas **responde 200** e sinaliza `body_too_large` (em vez do 413 do Kestrel). |

**Booleanos** aceitam `true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off` e `sim`/`não`, sem diferenciar maiúsculas.

**Valores inválidos** (número zero ou negativo, texto onde se espera número, booleano desconhecido) **impedem a subida do container** com uma mensagem que diz qual variável está errada. O echo não assume o padrão sem avisar.

Exemplo de container "silencioso", sem expor o ambiente e com histórico menor:

```sh
docker run -d -p 81:80 \
  -e ECHO_SHOW_ENVIRONMENT=false \
  -e ECHO_MAX_REQUESTS=200 \
  luizcarlosfaria/echo-server:latest
```

## Usando o echo

### HTML ou JSON

| Cliente | `Accept` | Resposta |
|---------|----------|----------|
| Navegador | `text/html,…` | Página HTML |
| curl | `*/*` | JSON |
| HttpClient, fetch, SDKs… | ausente, `application/json` etc. | JSON |

Quem pede `text/html` (ou `application/xhtml+xml`) recebe HTML. Todos os outros recebem JSON.

### Exemplo

```sh
curl -X POST 'http://localhost:81/pedidos?origem=readme' \
  -H 'Content-Type: application/json' \
  -H 'X-Correlation-Id: abc-123' \
  -d '{"cliente":"Ana","itens":[1,2]}'
```

```jsonc
{
  "RequestId": 61,
  "AppName": "Echo 1",
  "Now": "2026-11-21T00:38:27.6831246-03:00",
  "UtcNow": "2026-11-21T03:38:27.6831307Z",
  "Headers": {
    "Accept": [ "*/*" ],
    "Host": [ "localhost:81" ],
    "User-Agent": [ "curl/8.22.0" ],
    "Content-Type": [ "application/json" ],
    "Content-Length": [ "31" ],
    "X-Correlation-Id": [ "abc-123" ]
  },
  "Path": "/pedidos",
  "PathBase": "",
  "MachineName": "a1b2c3d4e5f6",
  "Method": "POST",
  "Scheme": "http",
  "Host": "localhost:81",
  "Protocol": "HTTP/1.1",
  "RemoteIp": "172.17.0.1",
  "Problems": [],
  "QueryString": [ { "Key": "origem", "Value": [ "readme" ] } ],
  "Forms": [],
  "Payload": { "cliente": "Ana", "itens": [ 1, 2 ] },
  "Body": {
    "Kind": "json",
    "Size": 31,
    "Truncated": false,
    "Charset": "utf-8",
    "Text": "{\"cliente\":\"Ana\",\"itens\":[1,2]}"
  },
  "EnvironmentVariables": { "APP_NAME": "Echo 1", "...": "..." }
}
```

### Campos da resposta

| Campo | Descrição |
|-------|-----------|
| `RequestId` | Id do request no histórico (também no header `X-Echo-Request-Id`). |
| `AppName`, `MachineName` | Identificam a instância. Úteis para validar o balanceamento. |
| `Now`, `UtcNow` | Horário do servidor (local, conforme `TZ`, e UTC). |
| `Method`, `Scheme`, `Host`, `Path`, `PathBase`, `Protocol` | Como o request chegou, **já com os `X-Forwarded-*` aplicados**. |
| `RemoteIp` | IP do cliente (via `X-Forwarded-For`, quando presente). |
| `Headers` | Todos os headers. Os originais substituídos por `X-Forwarded-*` aparecem como `X-Original-*`. |
| `QueryString` | Parâmetros da query, inclusive chaves repetidas. |
| `Forms` | Campos de formulário, no formato legado `[{ Key, Value }]`. |
| `Payload` | O JSON do body já interpretado (objeto, array ou primitivo), presente só quando o body é JSON válido. |
| `Body` | Detalhes do body; veja [formatos](#formatos-de-body-suportados). |
| `Problems` | Problemas detectados no request; veja [requests com problemas](#requests-com-problemas). |
| `EnvironmentVariables` | Variáveis de ambiente do container. Omitido quando `ECHO_SHOW_ENVIRONMENT=false`. |

Requests `HEAD` recebem os mesmos headers, sem body.

## Formatos de body suportados

| `Body.Kind` | Content-Types | Conteúdo devolvido |
|-------------|---------------|--------------------|
| `json` | `application/json`, `text/json`, `*/*+json` (ex.: `problem+json`, `vnd.api+json`) | `Text`, e `Payload` com o JSON interpretado |
| `xml` | `application/xml`, `text/xml`, `*/*+xml` (ex.: `soap+xml`) | `Text` |
| `text` | `text/*`, `application/javascript`, `x-ndjson`, `yaml`, `graphql`, `csv`… | `Text`, decodificado pelo `charset` informado (UTF-8, Latin-1…) |
| `form` | `application/x-www-form-urlencoded` | `Form` (e o legado `Forms`) |
| `multipart` | `multipart/form-data` | `Form` mais `Files` (nome, tipo e tamanho de cada arquivo, sem o conteúdo) |
| `binary` | demais tipos (`application/octet-stream`, imagens, PDF…) | `Base64` |
| `none` | sem body | n/a |

Além disso:

- **Sem `Content-Type`**, o formato é deduzido pelo conteúdo: JSON válido vira `json`, texto UTF-8 vira `text` e o resto vira `binary`.
- **Bodies chunked** (sem `Content-Length`) são lidos por inteiro.
- **Qualquer verbo** pode ter body, inclusive `GET` e `DELETE`.
- `Body.Size` é sempre o tamanho real recebido, mesmo quando o conteúdo armazenado foi truncado.

## Requests com problemas

O echo **não rejeita** requests malformados. Ele responde `200` com tudo o que conseguiu ler e sinaliza o que encontrou de errado:

- no JSON: `Problems: [{ "Code", "Severity", "Message" }]`;
- no header `X-Echo-Problems: json_invalid, charset_mismatch` (dá para ver com `curl -I`, sem ler o body);
- na página HTML, em um bloco destacado no topo;
- no painel `/_echo`, com badge e borda colorida na lista, filtro "Só com problemas" e detalhe de cada problema.

| Code | Severity | Quando acontece |
|------|----------|-----------------|
| `json_invalid` | error | O Content-Type diz JSON, mas o body não é um JSON válido. |
| `form_invalid` | error | Form ou multipart malformado (boundary ausente ou errado, limites de form excedidos). O body bruto é devolvido em `Text`/`Base64`. |
| `body_incomplete` | error | O body terminou antes do `Content-Length` declarado, ou o chunked está malformado. O que chegou é preservado. |
| `client_aborted` | error | O cliente derrubou a conexão no meio do envio. O request fica no histórico mesmo sem resposta possível. |
| `content_length_mismatch` | error | O `Content-Length` difere da quantidade de bytes recebida. |
| `body_too_large` | warning | O body passou de `ECHO_MAX_REQUEST_BYTES`, e a leitura foi interrompida. |
| `content_type_invalid` | warning | `Content-Type` ilegível. O formato do body é deduzido pelo conteúdo. |
| `charset_unknown` | warning | Charset desconhecido. O body é decodificado como UTF-8. |
| `charset_mismatch` | warning | Bytes inválidos para o charset declarado (ex.: Latin-1 enviado como UTF-8). |
| `body_truncated` | info | O body foi armazenado parcialmente (`ECHO_MAX_BODY_BYTES`). |

```sh
curl -si -X POST http://localhost:81/teste -H 'Content-Type: application/json' -d '{"cliente": ' | grep X-Echo
# X-Echo-Request-Id: 7
# X-Echo-Problems: json_invalid
```

## Painel em tempo real

Abra **`/_echo`** (ex.: http://localhost:81/_echo). Cada request recebido aparece instantaneamente, sem recarregar a página.

- **Lista ao vivo** com método, path, horário, tipo e tamanho do body, IP e problemas.
- **Detalhe** do request selecionado: problemas, dados do request, headers, query e body (JSON formatado, texto, campos de form, arquivos ou base64).
- **Filtro** por método, path, IP, content-type, Id ou código de problema.
- **Só com problemas**: mostra apenas os requests sinalizados.
- **Pausar/Retomar**: congela a lista. Os requests que chegam enquanto está pausado aparecem ao retomar.
- **Limpar histórico**: apaga o histórico no servidor e em todos os painéis abertos.
- **Reconexão automática**, com indicador de status (ao vivo, pausado ou desconectado).

### Atalhos de teclado

| Tecla | Ação |
|-------|------|
| <kbd>Home</kbd> | Seleciona o primeiro request (o mais recente). |
| <kbd>End</kbd> | Seleciona o último request (o mais antigo da lista). |
| <kbd>PgUp</kbd> / <kbd>PgDn</kbd> | Sobe ou desce uma página da lista. |
| <kbd>↑</kbd> / <kbd>↓</kbd> | Sobe ou desce um request. |

A navegação respeita o filtro ativo e mantém o item selecionado visível. Enquanto o cursor estiver no campo de filtro, as teclas continuam editando o texto normalmente.

## API do histórico e WebSocket

As rotas sob `/_echo` são **reservadas**: não são ecoadas nem entram no histórico.

| Rota | Descrição |
|------|-----------|
| `GET /_echo` | Painel em tempo real. |
| `GET /_echo/api/requests?limit=N` | Resumos do histórico, do mais recente para o mais antigo, com `capacity`, `count` e `totalReceived`. |
| `GET /_echo/api/requests/{id}` | Request completo (headers, query, body e problemas). Retorna `404` se ele já foi descartado. |
| `DELETE /_echo/api/requests` | Limpa o histórico. |
| `GET /_echo/ws` | WebSocket com os eventos em tempo real. |

### Protocolo do WebSocket

Mensagens JSON (camelCase), do servidor para o cliente:

```jsonc
// Ao conectar: o estado atual
{ "type": "snapshot", "capacity": 1000, "totalReceived": 42, "items": [ /* resumos */ ] }

// A cada request recebido
{ "type": "request", "totalReceived": 43, "item": {
    "id": 43, "timestamp": "2026-11-21T03:38:27Z", "method": "POST", "scheme": "http",
    "host": "localhost:81", "path": "/pedidos", "queryString": "?origem=readme",
    "remoteIp": "172.17.0.1", "contentType": "application/json",
    "bodyKind": "json", "bodySize": 31, "bodyTruncated": false,
    "problems": [] } }   // com problemas: "problems": ["json_invalid"], "problemSeverity": "error"

// Quando o histórico é limpo
{ "type": "cleared", "totalReceived": 43 }
```

O WebSocket envia apenas **resumos**. O conteúdo completo é buscado sob demanda em `/_echo/api/requests/{id}`. Cada conexão tem sua própria fila, então um cliente lento perde eventos antigos, mas não atrasa o echo nem os outros clientes.

```sh
# acompanhar pelo terminal (exige websocat)
websocat ws://localhost:81/_echo/ws
```

## Atrás de um proxy reverso

O echo confia nos headers `X-Forwarded-*` vindos de **qualquer origem**, porque o objetivo é justamente mostrar o que o proxy enviou. Não o exponha diretamente à internet esperando que esses headers sejam confiáveis.

Para o painel funcionar atrás de um proxy, ele precisa repassar o upgrade de WebSocket. Com nginx:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    location / {
        proxy_pass         http://echo:80;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade $http_upgrade;
        proxy_set_header   Connection $connection_upgrade;
        proxy_set_header   Host $host;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        proxy_read_timeout 1h;   # o painel mantém o WebSocket aberto
    }
}
```

O repositório traz um exemplo completo com nginx em [`docker-compose.yml`](docker-compose.yml) e [`proxy/`](proxy/):

```sh
docker compose up --build
# http://localhost/        echo através do nginx
# http://localhost/_echo   painel através do nginx
```

### Exemplo: troubleshooting de ingress no Kubernetes

```sh
curl -s http://meu-ingress/echo1?a=1 -H 'Accept: application/json'
```

A resposta mostra o `Host`, `Scheme` e `RemoteIp` que o pod recebeu de fato, os headers que o ingress adicionou ou removeu (`X-Forwarded-*`, `X-Real-Ip`, `X-Original-*`) e, por `MachineName`, qual réplica atendeu.

## Desenvolvimento

Requer o **SDK do .NET 11** (fixado em [`global.json`](global.json)).

```sh
dotnet run --project echo-server                          # http://localhost:5080/_echo
dotnet test --project tests/EchoServer.IntegrationTests   # testes de integração
docker build -f echo-server/Dockerfile -t echo-server .   # imagem
```

> Se o `docker build` falhar no `dotnet restore` com o código **155**, a imagem `mcr.microsoft.com/dotnet/sdk:11.0` no cache local é antiga (anterior à GA). Atualize com `docker pull mcr.microsoft.com/dotnet/sdk:11.0` e `docker pull mcr.microsoft.com/dotnet/aspnet:11.0`.

### Estrutura

```text
echo-server/
├── Program.cs                     pipeline, configuração e rotas
├── Capture/                       leitura do request: formatos, problemas, resposta do echo
├── Storage/                       EchoOptions (variáveis de ambiente) e RequestStore (histórico)
├── Realtime/                      WebSocket /_echo/ws
├── Endpoints/                     API /_echo/api/*
├── Controllers/                   echo (catch-all) e painel
├── Views/                         páginas HTML (Razor + Tailwind)
└── wwwroot/_echo/dashboard.js     front-end do painel
tests/EchoServer.IntegrationTests/ testes de integração (xunit.v3)
proxy/                             nginx de exemplo
```

### Testes

Os testes de integração sobem a aplicação real com `WebApplicationFactory` e cobrem verbos, paths, formatos de body, escolha entre HTML e JSON, `X-Forwarded-*`, histórico e descarte, variáveis de ambiente, concorrência, WebSocket e requests com problemas. Os cenários que só existem numa conexão de verdade (`Content-Length` mentiroso, conexão derrubada, body acima do limite) rodam contra **Kestrel real**, enviando bytes crus por socket TCP.

## Limitações conhecidas

- **Histórico só em memória**: reiniciar o container apaga tudo, e cada réplica tem o próprio histórico.
- **Sem autenticação**: o painel, a API e o echo são abertos. Com `ECHO_SHOW_ENVIRONMENT=true` (o padrão), as variáveis de ambiente do container ficam visíveis para qualquer um.
- **Falhas rejeitadas pelo servidor HTTP** antes de a aplicação ver o request não podem ser sinalizadas: request line ou headers malformados (400), headers grandes demais (431), ou uma conexão que envia o request e fecha antes de ele começar a ser processado.
- **Arquivos de multipart**: só os metadados (nome, tipo e tamanho) são guardados, nunca o conteúdo.

## Paleta de cores

Valores aceitos em `HEADER_COLOR`:

[![Paleta de cores do Tailwind CSS](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Customizing-Colors-Tailwind-CSS.png)](https://tailwindcss.com/docs/customizing-colors)
