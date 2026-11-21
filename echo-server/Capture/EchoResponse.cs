using System.Collections;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Serialization;
using EchoServer.Storage;
using Microsoft.Net.Http.Headers;

namespace EchoServer.Capture;

public static class EchoJson
{
    /// <summary>Resposta do echo: PascalCase e indentada, compatível com o formato histórico.</summary>
    public static readonly JsonSerializerOptions Echo = new()
    {
        WriteIndented = true,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    };

    /// <summary>API do painel e WebSocket: camelCase compacto.</summary>
    public static readonly JsonSerializerOptions Api = new(JsonSerializerDefaults.Web)
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };
}

public static class EchoResponse
{
    /// <summary>
    /// Monta o JSON devolvido pelo echo. Mantém os campos históricos
    /// (AppName, Headers, QueryString, Forms, Payload, EnvironmentVariables...) e adiciona
    /// RequestId, Host, RemoteIp e Body.
    /// </summary>
    public static JsonObject Build(CapturedRequest request, EchoOptions options)
    {
        var body = request.Body;
        var result = new JsonObject
        {
            ["RequestId"] = request.Id,
            ["AppName"] = options.AppName,
            ["Now"] = DateTime.Now.ToString("o"),
            ["UtcNow"] = DateTime.UtcNow.ToString("o"),
            ["Headers"] = JsonSerializer.SerializeToNode(request.Headers, EchoJson.Echo),
            ["Path"] = request.Path,
            ["PathBase"] = request.PathBase,
            ["MachineName"] = Environment.MachineName,
            ["Method"] = request.Method,
            ["Scheme"] = request.Scheme,
            ["Host"] = request.Host,
            ["Protocol"] = request.Protocol,
            ["RemoteIp"] = request.RemoteIp,
            ["Problems"] = JsonSerializer.SerializeToNode(request.Problems, EchoJson.Echo),
            ["QueryString"] = KeyValueArray(request.Query),
            ["Forms"] = KeyValueArray(body.Form ?? new Dictionary<string, string?[]>()),
        };

        if (body.Json is { } payload)
            result["Payload"] = JsonSerializer.SerializeToNode(payload);

        // O JSON já aparece em Payload; não duplica dentro de Body.
        result["Body"] = JsonSerializer.SerializeToNode(body with { Json = null }, EchoJson.Echo);
        if (options.ShowEnvironment)
            result["EnvironmentVariables"] = JsonSerializer.SerializeToNode(EnvironmentSnapshot.Get(), EchoJson.Echo);

        return result;
    }

    /// <summary>Formato legado: [{ "Key": "a", "Value": ["1", "2"] }].</summary>
    private static JsonArray KeyValueArray(IReadOnlyDictionary<string, string?[]> source)
    {
        var array = new JsonArray();
        foreach (var (key, values) in source)
            array.Add(new JsonObject { ["Key"] = key, ["Value"] = JsonSerializer.SerializeToNode(values) });
        return array;
    }

    /// <summary>Navegadores pedem text/html; curl, HttpClient e afins recebem JSON.</summary>
    public static bool WantsHtml(HttpRequest request)
    {
        var accept = request.GetTypedHeaders().Accept;
        return accept.Any(media =>
            (media.MediaType.Equals("text/html", StringComparison.OrdinalIgnoreCase)
             || media.MediaType.Equals("application/xhtml+xml", StringComparison.OrdinalIgnoreCase))
            && media.Quality is not 0);
    }
}

public static class EnvironmentSnapshot
{
    public static SortedDictionary<string, string?> Get()
    {
        var result = new SortedDictionary<string, string?>(StringComparer.Ordinal);
        foreach (DictionaryEntry entry in Environment.GetEnvironmentVariables())
            result[(string)entry.Key] = entry.Value as string;
        return result;
    }
}
