using System.Net;
using System.Net.Http.Headers;
using System.Text.Json;
using EchoServer.IntegrationTests.Infrastructure;
using static EchoServer.IntegrationTests.Infrastructure.EchoClientExtensions;

namespace EchoServer.IntegrationTests;

public class EchoVerbsAndNegotiationTests(EchoAppFactory factory) : IClassFixture<EchoAppFactory>
{
    private readonly HttpClient client = factory.CreateClient();

    public static TheoryData<string> Verbs => new() { "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS" };

    [Theory]
    [MemberData(nameof(Verbs))]
    public async Task Any_verb_is_echoed(string verb)
    {
        var echo = await this.client.EchoAsync(new HttpMethod(verb), "/qualquer/coisa");

        Assert.Equal(verb, echo.Str("Method"));
        Assert.Equal("/qualquer/coisa", echo.Str("Path"));
        Assert.Equal("Echo Tests", echo.Str("AppName"));
    }

    [Theory]
    [InlineData("/")]
    [InlineData("/a/b/c")]
    [InlineData("/arquivo.txt")]
    [InlineData("/api/v1/users/42.json")]
    [InlineData("/com%20espa%C3%A7o")]
    public async Task Any_path_is_echoed(string path)
    {
        var echo = await this.client.EchoAsync(HttpMethod.Get, path);

        Assert.Equal(Uri.UnescapeDataString(path), echo.Str("Path"));
    }

    [Fact]
    public async Task Head_returns_200_without_body()
    {
        using var response = await this.client.SendAsync(new HttpRequestMessage(HttpMethod.Head, "/head"), Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Empty(await response.Content.ReadAsByteArrayAsync(Ct));
    }

    [Theory]
    [InlineData("*/*")]
    [InlineData("application/json")]
    [InlineData("application/json, text/plain, */*")]
    [InlineData(null)]
    public async Task Non_html_accept_returns_json(string? accept)
    {
        var request = new HttpRequestMessage(HttpMethod.Get, "/negociacao");
        if (accept is not null)
            request.Headers.TryAddWithoutValidation("Accept", accept);

        using var response = await this.client.SendAsync(request, Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal("application/json", response.Content.Headers.ContentType?.MediaType);
    }

    [Theory]
    [InlineData("text/html")]
    [InlineData("text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")]
    public async Task Browser_accept_returns_html(string accept)
    {
        var request = new HttpRequestMessage(HttpMethod.Get, "/pagina?x=1");
        request.Headers.TryAddWithoutValidation("Accept", accept);

        using var response = await this.client.SendAsync(request, Ct);
        var html = await response.Content.ReadAsStringAsync(Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal("text/html", response.Content.Headers.ContentType?.MediaType);
        Assert.Contains("Echo Tests", html);
        Assert.Contains("/pagina", html);
    }

    [Fact]
    public async Task Html_is_rendered_for_post_with_json_body()
    {
        var request = new HttpRequestMessage(HttpMethod.Post, "/html-post")
        {
            Content = new StringContent("""{"nome":"<script>alert(1)</script>"}""", System.Text.Encoding.UTF8, "application/json"),
        };
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("text/html"));

        using var response = await this.client.SendAsync(request, Ct);
        var html = await response.Content.ReadAsStringAsync(Ct);

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.DoesNotContain("<script>alert(1)</script>", html);
        Assert.Contains("&lt;script&gt;", html);
    }

    [Fact]
    public async Task Query_string_with_repeated_keys_is_echoed()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Get, "/q?a=1&a=2&b=x%20y");

        var query = echo.GetProperty("QueryString").EnumerateArray()
            .ToDictionary(it => it.Str("Key"), it => it.GetProperty("Value").EnumerateArray().Select(v => v.GetString()).ToArray());

        Assert.Equal(["1", "2"], query["a"]);
        Assert.Equal(["x y"], query["b"]);
    }

    [Fact]
    public async Task Custom_headers_are_echoed()
    {
        var request = new HttpRequestMessage(HttpMethod.Get, "/headers");
        request.Headers.Add("X-Correlation-Id", "abc-123");

        var echo = await this.client.EchoAsync(request);

        Assert.Equal("abc-123", echo.GetProperty("Headers").GetProperty("X-Correlation-Id")[0].GetString());
    }

    [Fact]
    public async Task Forwarded_headers_are_applied()
    {
        var request = new HttpRequestMessage(HttpMethod.Get, "/forwarded");
        request.Headers.Add("X-Forwarded-Proto", "https");
        request.Headers.Add("X-Forwarded-Host", "api.exemplo.com");
        request.Headers.Add("X-Forwarded-For", "203.0.113.7");

        var echo = await this.client.EchoAsync(request);

        Assert.Equal("https", echo.Str("Scheme"));
        Assert.Equal("api.exemplo.com", echo.Str("Host"));
        Assert.Equal("203.0.113.7", echo.Str("RemoteIp"));
    }

    [Fact]
    public async Task Environment_variables_are_echoed()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Get, "/env");

        Assert.Equal(JsonValueKind.Object, echo.GetProperty("EnvironmentVariables").ValueKind);
        Assert.Equal(Environment.MachineName, echo.Str("MachineName"));
    }
}
