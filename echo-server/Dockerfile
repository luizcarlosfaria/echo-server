#See https://aka.ms/containerfastmode to understand how Visual Studio uses this Dockerfile to build your images for faster debugging.

FROM mcr.microsoft.com/dotnet/aspnet:11.0 AS base
WORKDIR /app
ENV TZ=America/Sao_Paulo \
    # Imagens .NET 8+ escutam em 8080 por padrão; mantemos 80 por compatibilidade (docker run -p 81:80).
    ASPNETCORE_HTTP_PORTS=80 \
    # Quantidade de requests mantidos em memória para o painel /_echo (expurga os mais antigos).
    ECHO_MAX_REQUESTS=1000 \
    # Bytes de body armazenados por request (o excedente é truncado; o echo informa o tamanho real).
    ECHO_MAX_BODY_BYTES=65536 \
    # Bytes lidos por body; acima disso o echo responde 200 e sinaliza body_too_large (em vez de 413).
    ECHO_MAX_REQUEST_BYTES=30000000 \
    # Exibe as variáveis de ambiente no echo; use false se elas puderem conter segredos.
    ECHO_SHOW_ENVIRONMENT=true
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
EXPOSE 80

FROM mcr.microsoft.com/dotnet/sdk:11.0 AS build
WORKDIR /src
COPY ["global.json", "./"]
COPY ["echo-server/EchoServer.csproj", "echo-server/"]
RUN dotnet restore "echo-server/EchoServer.csproj"
COPY . .
WORKDIR "/src/echo-server"
RUN dotnet build "EchoServer.csproj" -c Release -o /app/build --no-restore

FROM build AS publish
RUN dotnet publish "EchoServer.csproj" -c Release -o /app/publish --no-restore /p:UseAppHost=false

FROM base AS final
WORKDIR /app
COPY --from=publish /app/publish .
ENTRYPOINT ["dotnet", "EchoServer.dll"]
