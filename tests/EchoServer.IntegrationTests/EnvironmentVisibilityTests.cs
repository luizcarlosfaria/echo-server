using System.Net.Http.Headers;
using EchoServer.IntegrationTests.Infrastructure;
using static EchoServer.IntegrationTests.Infrastructure.EchoClientExtensions;

namespace EchoServer.IntegrationTests;

/// <summary>ECHO_SHOW_ENVIRONMENT controla se as variáveis de ambiente aparecem no echo.</summary>
public class EnvironmentVisibilityTests
{
    private static EchoAppFactory Create(string? value)
        => new(value is null ? [] : new Dictionary<string, string?> { ["ECHO_SHOW_ENVIRONMENT"] = value });

    private static async Task<string> GetHtmlAsync(EchoAppFactory app)
    {
        var request = new HttpRequestMessage(HttpMethod.Get, "/pagina");
        request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("text/html"));
        using var response = await app.CreateClient().SendAsync(request, Ct);
        response.EnsureSuccessStatusCode();
        return await response.Content.ReadAsStringAsync(Ct);
    }

    [Theory]
    [InlineData(null)]       // default: exibe (compatível com versões anteriores)
    [InlineData("true")]
    [InlineData("1")]
    [InlineData("SIM")]
    public async Task Environment_is_shown(string? value)
    {
        await using var app = Create(value);

        var echo = await app.CreateClient().EchoAsync(HttpMethod.Get, "/env");

        Assert.True(echo.TryGetProperty("EnvironmentVariables", out _));
        Assert.Contains("Environment Variables", await GetHtmlAsync(app));
    }

    [Theory]
    [InlineData("false")]
    [InlineData("0")]
    [InlineData("no")]
    [InlineData("Off")]
    public async Task Environment_is_hidden(string value)
    {
        await using var app = Create(value);

        var echo = await app.CreateClient().EchoAsync(HttpMethod.Get, "/env");

        Assert.False(echo.TryGetProperty("EnvironmentVariables", out _));
        Assert.Equal(Environment.MachineName, echo.Str("MachineName")); // o resto do echo continua igual

        var html = await GetHtmlAsync(app);
        Assert.DoesNotContain("Environment Variables", html);
        Assert.DoesNotContain(Environment.GetEnvironmentVariable("PATH")!, html);
    }

    [Fact]
    public void Invalid_value_fails_on_startup()
    {
        using var app = Create("talvez");

        var error = Assert.ThrowsAny<Exception>(() => app.CreateClient());
        Assert.Contains("ECHO_SHOW_ENVIRONMENT", error.ToString());
    }
}
