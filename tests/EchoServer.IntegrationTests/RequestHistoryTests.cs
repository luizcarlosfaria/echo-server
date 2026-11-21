using System.Net;
using System.Net.Http.Json;
using System.Net.WebSockets;
using System.Text;
using System.Text.Json;
using EchoServer.IntegrationTests.Infrastructure;
using static EchoServer.IntegrationTests.Infrastructure.EchoClientExtensions;

namespace EchoServer.IntegrationTests;

/// <summary>
/// Histórico em memória, API do painel e WebSocket. Cada teste sobe sua própria
/// instância para que o histórico de um não interfira no outro.
/// </summary>
public class RequestHistoryTests
{
    private static EchoAppFactory Create(int? maxRequests = null, int? maxBodyBytes = null)
    {
        var settings = new Dictionary<string, string?>();
        if (maxRequests is not null) settings["ECHO_MAX_REQUESTS"] = maxRequests.ToString();
        if (maxBodyBytes is not null) settings["ECHO_MAX_BODY_BYTES"] = maxBodyBytes.ToString();
        return new EchoAppFactory(settings);
    }

    private static async Task<JsonElement> ListAsync(HttpClient client, string query = "")
        => (await client.GetFromJsonAsync<JsonElement>($"/_echo/api/requests{query}", Ct))!;

    private static long[] Ids(JsonElement list) => list.GetProperty("items").EnumerateArray().Select(i => i.GetProperty("id").GetInt64()).ToArray();

    [Fact]
    public async Task Each_request_is_stored_and_retrievable_by_id()
    {
        await using var app = Create();
        var client = app.CreateClient();

        var echo = await client.EchoAsync(HttpMethod.Post, "/pedidos?x=1", JsonContent.Create(new { valor = 10 }));
        var id = echo.GetProperty("RequestId").GetInt64();

        var stored = await client.GetFromJsonAsync<JsonElement>($"/_echo/api/requests/{id}", Ct);
        Assert.Equal("POST", stored.Str("method"));
        Assert.Equal("/pedidos", stored.Str("path"));
        Assert.Equal("?x=1", stored.Str("queryString"));
        Assert.Equal(10, stored.GetProperty("body").GetProperty("json").GetProperty("valor").GetInt32());
    }

    [Fact]
    public async Task Response_carries_request_id_header()
    {
        await using var app = Create();
        using var response = await app.CreateClient().GetAsync("/com-header", Ct);

        Assert.True(response.Headers.TryGetValues("X-Echo-Request-Id", out var values));
        Assert.Equal("1", values.Single());
    }

    [Fact]
    public async Task Default_capacity_is_1000()
    {
        await using var app = Create();
        var list = await ListAsync(app.CreateClient());

        Assert.Equal(1000, list.GetProperty("capacity").GetInt32());
    }

    [Fact]
    public async Task Oldest_requests_are_purged_when_limit_is_reached()
    {
        await using var app = Create(maxRequests: 5);
        var client = app.CreateClient();

        for (var i = 1; i <= 8; i++)
            await client.EchoAsync(HttpMethod.Get, $"/r{i}");

        var list = await ListAsync(client);
        Assert.Equal(5, list.GetProperty("capacity").GetInt32());
        Assert.Equal(5, list.GetProperty("count").GetInt32());
        Assert.Equal(8, list.GetProperty("totalReceived").GetInt64());
        Assert.Equal([8L, 7, 6, 5, 4], Ids(list)); // mais recentes primeiro

        using var purged = await client.GetAsync("/_echo/api/requests/3", Ct);
        Assert.Equal(HttpStatusCode.NotFound, purged.StatusCode);
    }

    [Fact]
    public async Task List_honors_limit()
    {
        await using var app = Create();
        var client = app.CreateClient();
        for (var i = 0; i < 4; i++)
            await client.EchoAsync(HttpMethod.Get, "/x");

        var list = await ListAsync(client, "?limit=2");
        Assert.Equal([4L, 3], Ids(list));
    }

    [Fact]
    public async Task Dashboard_routes_are_not_captured()
    {
        await using var app = Create();
        var client = app.CreateClient();

        await ListAsync(client);
        (await client.GetAsync("/_echo", Ct)).EnsureSuccessStatusCode();
        (await client.GetAsync("/_echo/dashboard.js", Ct)).EnsureSuccessStatusCode();
        using var unknown = await client.GetAsync("/_echo/nao-existe", Ct);

        Assert.Equal(HttpStatusCode.NotFound, unknown.StatusCode);
        Assert.Equal(0, (await ListAsync(client)).GetProperty("totalReceived").GetInt64());
    }

    [Fact]
    public async Task Dashboard_page_is_served()
    {
        await using var app = Create();
        using var response = await app.CreateClient().GetAsync("/_echo/", Ct);
        var html = await response.Content.ReadAsStringAsync(Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Contains("echo-dashboard", html);
        Assert.Contains("dashboard.js", html);
    }

    [Fact]
    public async Task Clear_removes_everything()
    {
        await using var app = Create();
        var client = app.CreateClient();
        await client.EchoAsync(HttpMethod.Get, "/a");

        using var deleted = await client.DeleteAsync("/_echo/api/requests", Ct);

        Assert.Equal(HttpStatusCode.NoContent, deleted.StatusCode);
        Assert.Equal(0, (await ListAsync(client)).GetProperty("count").GetInt32());
    }

    [Fact]
    public async Task Large_bodies_are_truncated_but_size_is_preserved()
    {
        await using var app = Create(maxBodyBytes: 100);
        var client = app.CreateClient();
        var text = new string('x', 1000);

        var echo = await client.EchoAsync(HttpMethod.Post, "/grande", new StringContent(text, Encoding.UTF8, "text/plain"));

        var body = echo.Body();
        Assert.True(body.GetProperty("Truncated").GetBoolean());
        Assert.Equal(1000, body.GetProperty("Size").GetInt64());
        Assert.Equal(100, body.Str("Text").Length);
    }

    [Fact]
    public async Task Truncated_json_is_not_parsed()
    {
        await using var app = Create(maxBodyBytes: 10);
        var echo = await app.CreateClient().EchoAsync(HttpMethod.Post, "/json-grande", JsonContent.Create(new { campo = new string('y', 100) }));

        Assert.False(echo.TryGetProperty("Payload", out _));
        Assert.Contains("truncated", echo.Body().Str("JsonError"));
    }

    [Theory]
    [InlineData("0")]
    [InlineData("-1")]
    [InlineData("abc")]
    public void Invalid_capacity_fails_on_startup(string value)
    {
        using var app = new EchoAppFactory(new Dictionary<string, string?> { ["ECHO_MAX_REQUESTS"] = value });

        var error = Assert.ThrowsAny<Exception>(() => app.CreateClient());
        Assert.Contains("ECHO_MAX_REQUESTS", error.ToString());
    }

    [Fact]
    public async Task Concurrent_requests_are_all_stored_with_unique_ids()
    {
        await using var app = Create(maxRequests: 100);
        var client = app.CreateClient();

        var ids = await Task.WhenAll(Enumerable.Range(0, 50).Select(async i =>
            (await client.EchoAsync(HttpMethod.Get, $"/c{i}")).GetProperty("RequestId").GetInt64()));

        Assert.Equal(50, ids.Distinct().Count());
        Assert.Equal(50, (await ListAsync(client)).GetProperty("count").GetInt32());
    }

    // ---------------------------------------------------------------- WebSocket

    [Fact]
    public async Task WebSocket_sends_snapshot_then_live_requests()
    {
        await using var app = Create();
        var client = app.CreateClient();
        await client.EchoAsync(HttpMethod.Get, "/antes");

        using var socket = await app.Server.CreateWebSocketClient().ConnectAsync(new Uri("ws://localhost/_echo/ws"), Ct);

        var snapshot = await ReceiveJsonAsync(socket);
        Assert.Equal("snapshot", snapshot.Str("type"));
        Assert.Equal(1000, snapshot.GetProperty("capacity").GetInt32());
        Assert.Equal("/antes", snapshot.GetProperty("items")[0].Str("path"));

        await client.EchoAsync(HttpMethod.Post, "/depois", JsonContent.Create(new { a = 1 }));

        var live = await ReceiveJsonAsync(socket);
        Assert.Equal("request", live.Str("type"));
        Assert.Equal("/depois", live.GetProperty("item").Str("path"));
        Assert.Equal("POST", live.GetProperty("item").Str("method"));
        Assert.Equal("json", live.GetProperty("item").Str("bodyKind"));
        Assert.Equal(2, live.GetProperty("totalReceived").GetInt64());

        await client.DeleteAsync("/_echo/api/requests", Ct);
        Assert.Equal("cleared", (await ReceiveJsonAsync(socket)).Str("type"));

        await socket.CloseAsync(WebSocketCloseStatus.NormalClosure, "fim", Ct);
    }

    [Fact]
    public async Task WebSocket_endpoint_rejects_plain_http()
    {
        await using var app = Create();
        using var response = await app.CreateClient().GetAsync("/_echo/ws", Ct);

        Assert.Equal(HttpStatusCode.BadRequest, response.StatusCode);
    }

    [Fact]
    public async Task Multiple_websocket_clients_receive_the_same_events()
    {
        await using var app = Create();
        var wsClient = app.Server.CreateWebSocketClient();
        using var first = await wsClient.ConnectAsync(new Uri("ws://localhost/_echo/ws"), Ct);
        using var second = await wsClient.ConnectAsync(new Uri("ws://localhost/_echo/ws"), Ct);
        await ReceiveJsonAsync(first);
        await ReceiveJsonAsync(second);

        await app.CreateClient().EchoAsync(HttpMethod.Get, "/broadcast");

        Assert.Equal("/broadcast", (await ReceiveJsonAsync(first)).GetProperty("item").Str("path"));
        Assert.Equal("/broadcast", (await ReceiveJsonAsync(second)).GetProperty("item").Str("path"));
    }

    private static async Task<JsonElement> ReceiveJsonAsync(WebSocket socket)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(Ct);
        timeout.CancelAfter(TimeSpan.FromSeconds(10));

        using var message = new MemoryStream();
        var buffer = new byte[4096];
        WebSocketReceiveResult result;
        do
        {
            result = await socket.ReceiveAsync(buffer, timeout.Token);
            message.Write(buffer, 0, result.Count);
        } while (!result.EndOfMessage);

        return JsonDocument.Parse(message.ToArray()).RootElement.Clone();
    }
}
