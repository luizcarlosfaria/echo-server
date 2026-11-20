using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using EchoServer.IntegrationTests.Infrastructure;
using static EchoServer.IntegrationTests.Infrastructure.EchoClientExtensions;

namespace EchoServer.IntegrationTests;

/// <summary>
/// Requests com falhas são aceitos (200) e os problemas são sinalizados em
/// <c>Problems</c>, no header <c>X-Echo-Problems</c>, no HTML e na API do painel.
/// </summary>
public class RequestProblemTests(EchoAppFactory factory) : IClassFixture<EchoAppFactory>
{
    private readonly HttpClient client = factory.CreateClient();

    private static JsonElement[] Problems(JsonElement echo) => echo.GetProperty("Problems").EnumerateArray().ToArray();

    private static JsonElement Problem(JsonElement echo, string code)
    {
        var problem = Problems(echo).SingleOrDefault(p => p.Str("Code") == code);
        Assert.True(problem.ValueKind == JsonValueKind.Object,
            $"Problema '{code}' não sinalizado. Recebidos: [{string.Join(", ", Problems(echo).Select(p => p.Str("Code")))}]");
        return problem;
    }

    private static ByteArrayContent Raw(byte[] bytes, string contentType)
    {
        var content = new ByteArrayContent(bytes);
        content.Headers.TryAddWithoutValidation("Content-Type", contentType);
        return content;
    }

    [Fact]
    public async Task Clean_request_has_no_problems()
    {
        using var response = await this.client.PostAsJsonAsync("/ok", new { a = 1 }, Ct);
        var echo = JsonDocument.Parse(await response.Content.ReadAsStringAsync(Ct)).RootElement;

        Assert.Empty(Problems(echo));
        Assert.False(response.Headers.Contains("X-Echo-Problems"));
    }

    [Fact]
    public async Task Invalid_json_is_accepted_and_flagged_as_error()
    {
        using var response = await this.client.PostAsync("/json", Raw("{ \"a\": "u8.ToArray(), "application/json"), Ct);
        var echo = JsonDocument.Parse(await response.Content.ReadAsStringAsync(Ct)).RootElement;

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        var problem = Problem(echo, "json_invalid");
        Assert.Equal("error", problem.Str("Severity"));
        Assert.Contains("JSON inválido", problem.Str("Message"));
        Assert.Equal("json_invalid", response.Headers.GetValues("X-Echo-Problems").Single());
    }

    [Fact]
    public async Task Malformed_multipart_is_flagged()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/upload", Raw("sem boundary nenhum"u8.ToArray(), "multipart/form-data; boundary=xyz"));

        Assert.Equal("error", Problem(echo, "form_invalid").Str("Severity"));
        Assert.Equal("sem boundary nenhum", echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Multipart_without_boundary_is_flagged()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/upload", Raw("abc"u8.ToArray(), "multipart/form-data"));

        Problem(echo, "form_invalid");
    }

    [Fact]
    public async Task Unparseable_content_type_is_flagged_and_body_is_sniffed()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/ct", Raw("""{"ok":true}"""u8.ToArray(), "isto nao e um content type"));

        Assert.Equal("warning", Problem(echo, "content_type_invalid").Str("Severity"));
        Assert.True(echo.GetProperty("Payload").GetProperty("ok").GetBoolean());
    }

    [Fact]
    public async Task Unknown_charset_is_flagged_and_decoded_as_utf8()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/charset", Raw("olá"u8.ToArray(), "text/plain; charset=klingon"));

        Problem(echo, "charset_unknown");
        Assert.Equal("olá", echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Bytes_invalid_for_declared_charset_are_flagged()
    {
        var latin1 = Encoding.Latin1.GetBytes("ação");

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/charset", Raw(latin1, "text/plain; charset=utf-8"));

        Problem(echo, "charset_mismatch");
        Assert.Contains('�', echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Content_length_mismatch_is_flagged()
    {
        var content = new LyingContent(Encoding.UTF8.GetBytes("0123456789"), declaredLength: 25);
        content.Headers.ContentType = new MediaTypeHeaderValue("text/plain");

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/cl", content);

        Assert.Contains("25", Problem(echo, "content_length_mismatch").Str("Message"));
        Assert.Equal(10, echo.Body().GetProperty("Size").GetInt64());
    }

    [Fact]
    public async Task Truncated_storage_is_flagged_as_info()
    {
        await using var app = new EchoAppFactory(new Dictionary<string, string?> { ["ECHO_MAX_BODY_BYTES"] = "10" });

        var echo = await app.CreateClient().EchoAsync(HttpMethod.Post, "/grande", new StringContent(new string('a', 100)));

        Assert.Equal("info", Problem(echo, "body_truncated").Str("Severity"));
    }

    [Theory]
    [InlineData("text/plain")]
    [InlineData("application/x-www-form-urlencoded")]
    [InlineData("multipart/form-data; boundary=abc")]
    public async Task Oversized_body_is_accepted_partially_instead_of_413(string contentType)
    {
        await using var app = new EchoAppFactory(new Dictionary<string, string?> { ["ECHO_MAX_REQUEST_BYTES"] = "1000" });

        var echo = await app.CreateClient().EchoAsync(HttpMethod.Post, "/enorme", Raw(new byte[5000].Select(_ => (byte)'a').ToArray(), contentType));

        Assert.Equal("warning", Problem(echo, "body_too_large").Str("Severity"));
        Assert.Equal(1000, echo.Body().GetProperty("Size").GetInt64());
        Assert.DoesNotContain(Problems(echo), p => p.Str("Code") == "content_length_mismatch");
    }

    [Fact]
    public async Task Multiple_problems_are_reported_together()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/varios", Raw(Encoding.Latin1.GetBytes("{ ação"), "application/json; charset=utf-8"));

        var codes = Problems(echo).Select(p => p.Str("Code")).ToArray();
        Assert.Contains("charset_mismatch", codes);
        Assert.Contains("json_invalid", codes);
    }

    [Fact]
    public async Task Problems_are_exposed_to_the_dashboard()
    {
        await using var app = new EchoAppFactory();
        var http = app.CreateClient();
        var echo = await http.EchoAsync(HttpMethod.Post, "/painel", Raw("{x"u8.ToArray(), "application/json"));
        var id = echo.GetProperty("RequestId").GetInt64();

        var list = await http.GetFromJsonAsync<JsonElement>("/_echo/api/requests", Ct);
        var summary = list.GetProperty("items")[0];
        Assert.Equal(["json_invalid"], summary.GetProperty("problems").EnumerateArray().Select(p => p.GetString()).ToArray());
        Assert.Equal("error", summary.Str("problemSeverity"));

        var detail = await http.GetFromJsonAsync<JsonElement>($"/_echo/api/requests/{id}", Ct);
        Assert.Equal("json_invalid", detail.GetProperty("problems")[0].Str("code"));
    }

    [Fact]
    public async Task Clean_summary_has_no_severity()
    {
        await using var app = new EchoAppFactory();
        var http = app.CreateClient();
        await http.EchoAsync(HttpMethod.Get, "/limpo");

        var summary = (await http.GetFromJsonAsync<JsonElement>("/_echo/api/requests", Ct)).GetProperty("items")[0];

        Assert.Equal(0, summary.GetProperty("problems").GetArrayLength());
        Assert.False(summary.TryGetProperty("problemSeverity", out _));
    }

    [Fact]
    public async Task Html_page_lists_problems()
    {
        var request = new HttpRequestMessage(HttpMethod.Post, "/html") { Content = Raw("{x"u8.ToArray(), "application/json") };
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("text/html"));

        using var response = await this.client.SendAsync(request, Ct);
        var html = await response.Content.ReadAsStringAsync(Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Contains("Problemas detectados", html);
        Assert.Contains("json_invalid", html);
    }

    /// <summary>Declara um Content-Length diferente do que realmente envia.</summary>
    private sealed class LyingContent(byte[] data, long declaredLength) : HttpContent
    {
        protected override Task SerializeToStreamAsync(Stream stream, TransportContext? context) => stream.WriteAsync(data).AsTask();

        protected override bool TryComputeLength(out long length)
        {
            length = declaredLength;
            return true;
        }
    }
}
