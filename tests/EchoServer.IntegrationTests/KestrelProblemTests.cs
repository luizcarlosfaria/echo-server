using System.Net;
using System.Net.Http.Json;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using EchoServer.IntegrationTests.Infrastructure;
using Microsoft.AspNetCore.Hosting.Server;
using Microsoft.AspNetCore.Hosting.Server.Features;
using Microsoft.Extensions.DependencyInjection;
using static EchoServer.IntegrationTests.Infrastructure.EchoClientExtensions;

namespace EchoServer.IntegrationTests;

/// <summary>
/// Falhas que só existem numa conexão de verdade (o TestServer não as reproduz):
/// sobe o EchoServer no Kestrel em uma porta livre e conversa por socket TCP cru.
/// </summary>
public class KestrelProblemTests
{
    private static (EchoAppFactory App, Uri BaseAddress) StartKestrel(Dictionary<string, string?>? settings = null)
    {
        var app = new EchoAppFactory(settings ?? []);
        app.UseKestrel(0);
        app.StartServer();

        var address = app.Services.GetRequiredService<IServer>().Features.Get<IServerAddressesFeature>()!.Addresses.First();
        return (app, new Uri(address));
    }

    private static async Task<Socket> ConnectAsync(Uri baseAddress)
    {
        var socket = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
        await socket.ConnectAsync(IPAddress.Loopback, baseAddress.Port, Ct);
        return socket;
    }

    private static Task SendAsync(Socket socket, string text) => socket.SendAsync(Encoding.UTF8.GetBytes(text), SocketFlags.None, Ct).AsTask();

    private static async Task<string> ReadToEndAsync(Socket socket)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(Ct);
        timeout.CancelAfter(TimeSpan.FromSeconds(10));

        var buffer = new byte[8192];
        var response = new StringBuilder();
        int read;
        while ((read = await socket.ReceiveAsync(buffer, SocketFlags.None, timeout.Token)) > 0)
            response.Append(Encoding.UTF8.GetString(buffer, 0, read));
        return response.ToString();
    }

    [Fact]
    public async Task Body_shorter_than_content_length_is_kept_in_history_as_incomplete()
    {
        var (app, baseAddress) = StartKestrel();
        await using var _ = app;
        using (var socket = await ConnectAsync(baseAddress))
        {
            await SendAsync(socket,
                "POST /incompleto HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\n" +
                "Content-Type: text/plain\r\nContent-Length: 100\r\n\r\n" +
                "apenas 20 bytes aqui");
            // Cliente "trava" e depois desiste. (Se o FIN chegar junto com os dados, antes de o Kestrel
            // iniciar a conexão, o próprio Kestrel descarta tudo sem chamar a aplicação.)
            await Task.Delay(300, Ct);
            socket.Shutdown(SocketShutdown.Send); // FIN antes de completar o Content-Length

            // O Kestrel encerra a conexão nesse caso: não existe resposta possível para o cliente.
            Assert.Equal(string.Empty, await ReadToEndAsync(socket));
        }

        // ...mas o request não se perde: fica no histórico, com o que chegou e o problema sinalizado.
        using var http = new HttpClient { BaseAddress = baseAddress };
        var summary = await WaitForAsync(http, item => item.Str("path") == "/incompleto");
        Assert.Contains("body_incomplete", summary.GetProperty("problems").EnumerateArray().Select(p => p.GetString()));
        Assert.Equal("error", summary.Str("problemSeverity"));

        var detail = await http.GetFromJsonAsync<JsonElement>($"/_echo/api/requests/{summary.GetProperty("id").GetInt64()}", Ct);
        Assert.Equal("apenas 20 bytes aqui", detail.GetProperty("body").Str("text"));
        Assert.Contains("after 20 bytes",detail.GetProperty("problems")[0].Str("message"));
    }

    [Fact]
    public async Task Aborted_upload_is_kept_in_history_as_client_aborted()
    {
        var (app, baseAddress) = StartKestrel();
        await using var _ = app;

        using (var socket = await ConnectAsync(baseAddress))
        {
            await SendAsync(socket,
                "POST /abortado HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 5000\r\n\r\n" +
                "{\"parcial\":");
            await Task.Delay(200, Ct);              // garante que o servidor começou a ler o body
            socket.LingerState = new LingerOption(true, 0);
            socket.Close();                          // RST: queda abrupta da conexão
        }

        using var http = new HttpClient { BaseAddress = baseAddress };
        var stored = await WaitForAsync(http, item => item.Str("path") == "/abortado");

        var problems = stored.GetProperty("problems").EnumerateArray().Select(p => p.GetString()).ToArray();
        Assert.True(problems.Contains("client_aborted") || problems.Contains("body_incomplete"),
            $"Esperado client_aborted/body_incomplete, recebido: [{string.Join(", ", problems)}]");
        Assert.Equal("error", stored.Str("problemSeverity"));
        Assert.Equal(11, stored.GetProperty("bodySize").GetInt64());
    }

    [Fact]
    public async Task Body_over_limit_gets_200_instead_of_413()
    {
        var (app, baseAddress) = StartKestrel(new() { ["ECHO_MAX_REQUEST_BYTES"] = "1024" });
        await using var _ = app;
        using var http = new HttpClient { BaseAddress = baseAddress };

        using var response = await http.PostAsync("/upload-gigante", new ByteArrayContent(new byte[256 * 1024]), Ct);
        var echo = JsonDocument.Parse(await response.Content.ReadAsStringAsync(Ct)).RootElement;

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Contains("body_too_large", response.Headers.GetValues("X-Echo-Problems").Single());
        Assert.Equal(1024, echo.Body().GetProperty("Size").GetInt64());
    }

    [Fact]
    public async Task Body_larger_than_old_kestrel_default_is_accepted()
    {
        // Antes: Kestrel respondia 413 acima de 30 MB sem o request chegar à aplicação.
        var (app, baseAddress) = StartKestrel(new() { ["ECHO_MAX_REQUEST_BYTES"] = "40000000" });
        await using var _ = app;
        using var http = new HttpClient { BaseAddress = baseAddress };

        using var response = await http.PutAsync("/31mb", new ByteArrayContent(new byte[31_000_000]), Ct);
        var echo = JsonDocument.Parse(await response.Content.ReadAsStringAsync(Ct)).RootElement;

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal(31_000_000, echo.Body().GetProperty("Size").GetInt64());
        Assert.DoesNotContain(echo.GetProperty("Problems").EnumerateArray(), p => p.Str("Code") == "body_too_large");
    }

    private static async Task<JsonElement> WaitForAsync(HttpClient http, Func<JsonElement, bool> predicate)
    {
        var deadline = DateTime.UtcNow.AddSeconds(10);
        var last = default(JsonElement);
        while (DateTime.UtcNow < deadline)
        {
            last = await http.GetFromJsonAsync<JsonElement>("/_echo/api/requests", Ct);
            var match = last.GetProperty("items").EnumerateArray().FirstOrDefault(predicate);
            if (match.ValueKind == JsonValueKind.Object)
                return match;
            await Task.Delay(100, Ct);
        }
        throw new TimeoutException($"O request não apareceu no histórico. Último estado: {last}");
    }
}
