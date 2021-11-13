#See https://aka.ms/containerfastmode to understand how Visual Studio uses this Dockerfile to build your images for faster debugging.

FROM mcr.microsoft.com/dotnet/aspnet:5.0-focal AS base
WORKDIR /app
ENV TZ=America/Sao_Paulo
RUN cat /usr/share/zoneinfo/$TZ > /etc/localtime \
&& cat /usr/share/zoneinfo/$TZ > /etc/timezone
EXPOSE 80

FROM mcr.microsoft.com/dotnet/sdk:5.0-focal AS build
WORKDIR /src
COPY ["echo-server/EchoServer.csproj", "echo-server/"]
RUN dotnet restore "echo-server/EchoServer.csproj"
COPY . .
WORKDIR "/src/echo-server"
RUN dotnet build "EchoServer.csproj" -c Release -o /app/build

FROM build AS publish
RUN dotnet publish "EchoServer.csproj" -c Release -o /app/publish

FROM base AS final
WORKDIR /app
COPY --from=publish /app/publish .
ENTRYPOINT ["dotnet", "EchoServer.dll"]