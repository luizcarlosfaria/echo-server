# echo-server
Echo Server - serviço de teste para Proxy Reverso e API Gateway

Antes de subir sua aplicação, valide.


| Echo 1  | Echo 2 |
| ------------- | ------------- |
| ![Isso é uma imagem](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Echo-Server1.png)  | ![Isso é uma imagem](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Echo-Server2.png) |
| **EnvVars**  | **EnvVars**  |
| HEADER_COLOR=blue-600  | HEADER_COLOR=red-600  |
| APP_NAME=Echo 1  |  APP_NAME=Echo 2 |


## Formas de Uso

```sh
docker run -d -p:81:80 \
-e HEADER_COLOR=blue-600 \
-e APP_NAME="Echo 1" \
luizcarlosfaria/echo-server:latest
```
Abra o navegador em http://localhost:81

```sh
docker run -d -p:82:80 \
-e HEADER_COLOR=red-600 \
-e APP_NAME="Echo 2" \
luizcarlosfaria/echo-server:latest
```
Abra o navegador em http://localhost:82




## Paleta de Cores

[![Isso é uma imagem](https://raw.githubusercontent.com/luizcarlosfaria/echo-server/master/docs/assets/Customizing-Colors-Tailwind-CSS.png)](https://tailwindcss.com/docs/customizing-colors)

## Usando EchoServer por linha de comando

Abaixo tenho um exemplo real de troubleshooting de ingress controller do Kubernetes.

```sh
curl -v  http://localhost/echo2?a=1 -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{}'



*   Trying 127.0.0.1:80...
* TCP_NODELAY set
* Connected to localhost (127.0.0.1) port 80 (#0)
> POST /echo1?a=1 HTTP/1.1
> Host: localhost
> User-Agent: curl/7.68.0
> Content-Type: application/json
> Accept: application/json
> Content-Length: 2
>
* upload completely sent off: 2 out of 2 bytes
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
< Content-Length: 2374
< Content-Type: application/json
< Date: Sat, 28 May 2022 08:31:20 GMT
< Server: Kestrel
<
{
  "AppName": "Echo 1",
  "Now": "2022-05-28T05:31:21.3113989-03:00",
  "UtcNow": "2022-05-28T08:31:21.3114091Z",
  "Headers": {
    "Content-Type": [
      "application/json"
    ],
    "Accept": [
      "application/json"
    ],
    "Accept-Encoding": [
      "gzip"
    ],
    "Host": [
      "localhost"
    ],
    "User-Agent": [
      "curl/7.68.0"
    ],
    "Content-Length": [
      "2"
    ],
    "X-Original-Proto": [
      "http"
    ],
    "X-Forwarded-Port": [
      "80"
    ],
    "X-Original-Host": [
      "localhost"
    ],
    "X-Forwarded-Server": [
      "traefik-56c4b88c4b-nvsqw"
    ],
    "X-Real-Ip": [
      "10.42.0.1"
    ],
    "X-Original-For": [
      "[::ffff:10.42.0.14]:49378"
    ]
  },
  "Path": "/echo1",
  "MachineName": "echo1-0",
  "Method": "POST",
  "Scheme": "http",
  "QueryString": [
    {
      "Key": "a",
      "Value": [
        "1"
      ]
    }
  ],
  "Forms": [],
  "Payload": {},
  "EnvironmentVariables": {
    "ECHO2_SERVICE_SERVICE_HOST": "10.43.85.153",
    "KUBERNETES_PORT": "tcp://10.43.0.1:443",
    "ECHO1_SERVICE_PORT_80_TCP_PORT": "80",
    "HOSTNAME": "echo1-0",
    "ECHO2_SERVICE_PORT_80_TCP_ADDR": "10.43.85.153",
    "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
    "ECHO1_SERVICE_SERVICE_HOST": "10.43.107.33",
    "KUBERNETES_SERVICE_PORT": "443",
    "TZ": "America/Sao_Paulo",
    "HEADER_COLOR": "blue-600",
    "DOTNET_VERSION": "5.0.12",
    "ECHO1_SERVICE_SERVICE_PORT": "80",
    "DOTNET_RUNNING_IN_CONTAINER": "true",
    "KUBERNETES_PORT_443_TCP": "tcp://10.43.0.1:443",
    "APP_NAME": "Echo 1",
    "KUBERNETES_SERVICE_PORT_HTTPS": "443",
    "ASPNETCORE_URLS": "http://+:80",
    "KUBERNETES_PORT_443_TCP_ADDR": "10.43.0.1",
    "ECHO1_SERVICE_PORT_80_TCP": "tcp://10.43.107.33:80",
    "ASPNET_VERSION": "5.0.12",
    "ECHO2_SERVICE_PORT_80_TCP_PROTO": "tcp",
    "KUBERNETES_PORT_443_TCP_PORT": "443",
    "KUBERNETES_PORT_443_TCP_PROTO": "tcp",
    "ECHO2_SERVICE_SERVICE_PORT": "80",
    "ECHO2_SERVICE_PORT": "tcp://10.43.85.153:80",
    "HOME": "/root",
    "ECHO1_SERVICE_PORT": "tcp://10.43.107.33:80",
    "ECHO2_SERVICE_PORT_80_TCP_PORT": "80",
    "ECHO2_SERVICE_PORT_80_TCP": "tcp://10.43.85.153:80",
    "ECHO1_SERVICE_PORT_80_TCP_ADDR": "10.43.107.33",
    "KUBERNETES_SERVICE_HOST": "10.43.0.1",
    "ECHO1_SERVICE_PORT_80_TCP_PROTO": "tcp"
  }
* Connection #0 to host localhost left intact

```
