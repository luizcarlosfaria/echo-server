using System.Net.Http.Headers;
using System.Text.Json;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Mvc.Testing;

namespace EchoServer.IntegrationTests.Infrastructure;

/// <summary>
/// Sobe o EchoServer em memória (TestServer). Configurações extras simulam
/// variáveis de ambiente passadas na subida do container.
/// </summary>
public class EchoAppFactory : WebApplicationFactory<Program>
{
    private readonly IReadOnlyDictionary<string, string?> settings;

    public EchoAppFactory() : this(new Dictionary<string, string?>()) { }

    // internal: fixtures do xUnit exigem um único construtor público.
    internal EchoAppFactory(IReadOnlyDictionary<string, string?> settings) => this.settings = settings;

    protected override void ConfigureWebHost(IWebHostBuilder builder)
    {
        // A pasta do projeto (echo-server) não tem o nome do assembly (EchoServer), então a
        // descoberta automática do content root falha e o wwwroot não seria encontrado.
        builder.UseContentRoot(FindProjectDirectory());
        builder.UseEnvironment("Testing");
        builder.UseSetting("APP_NAME", "Echo Tests");
        foreach (var (key, value) in this.settings)
            builder.UseSetting(key, value);
    }

    private static string FindProjectDirectory()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "echo-server");
            if (File.Exists(Path.Combine(candidate, "EchoServer.csproj")))
                return candidate;
        }
        throw new DirectoryNotFoundException("Não foi possível localizar echo-server/EchoServer.csproj a partir de " + AppContext.BaseDirectory);
    }
}

public static class EchoClientExtensions
{
    public static CancellationToken Ct => TestContext.Current.CancellationToken;

    /// <summary>Envia o request pedindo JSON e devolve o corpo do echo já parseado.</summary>
    public static async Task<JsonElement> EchoAsync(this HttpClient client, HttpRequestMessage request)
    {
        if (request.Headers.Accept.Count == 0)
            request.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));

        using var response = await client.SendAsync(request, Ct);
        var raw = await response.Content.ReadAsStringAsync(Ct);

        Assert.True(response.IsSuccessStatusCode, $"Status {(int)response.StatusCode}: {raw}");
        Assert.Equal("application/json", response.Content.Headers.ContentType?.MediaType);

        return JsonDocument.Parse(raw).RootElement.Clone();
    }

    public static Task<JsonElement> EchoAsync(this HttpClient client, HttpMethod method, string url, HttpContent? content = null)
        => client.EchoAsync(new HttpRequestMessage(method, url) { Content = content });

    public static JsonElement Body(this JsonElement echo) => echo.GetProperty("Body");

    public static string Str(this JsonElement element, string property) => element.GetProperty(property).GetString()!;
}
