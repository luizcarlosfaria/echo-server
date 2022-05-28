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
