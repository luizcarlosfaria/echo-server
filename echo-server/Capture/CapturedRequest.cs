using System.Text.Json;

namespace EchoServer.Capture;

/// <summary>
/// Fotografia imutável de um request recebido. É o que o echo devolve,
/// o que fica no histórico em memória e o que o painel exibe.
/// </summary>
public sealed record CapturedRequest
{
    public long Id { get; init; }
    public DateTimeOffset Timestamp { get; init; }
    public required string Method { get; init; }
    public required string Scheme { get; init; }
    public required string Host { get; init; }
    public required string PathBase { get; init; }
    public required string Path { get; init; }
    public required string QueryString { get; init; }
    public required IReadOnlyDictionary<string, string?[]> Query { get; init; }
    public required IReadOnlyDictionary<string, string?[]> Headers { get; init; }
    public string? RemoteIp { get; init; }
    public required string Protocol { get; init; }
    public string? ContentType { get; init; }
    public long? ContentLength { get; init; }
    public required BodyCapture Body { get; init; }

    /// <summary>Problemas detectados no request. Vazio quando está tudo certo.</summary>
    public IReadOnlyList<RequestProblem> Problems { get; init; } = [];

    public RequestSummary ToSummary() => new(
        this.Id, this.Timestamp, this.Method, this.Scheme, this.Host, this.Path, this.QueryString,
        this.RemoteIp, this.ContentType, this.Body.Kind, this.Body.Size, this.Body.Truncated,
        this.Problems.Select(p => p.Code).ToArray(), WorstSeverity(this.Problems));

    private static string? WorstSeverity(IReadOnlyList<RequestProblem> problems)
        => problems.Any(p => p.Severity == ProblemSeverity.Error) ? ProblemSeverity.Error
         : problems.Any(p => p.Severity == ProblemSeverity.Warning) ? ProblemSeverity.Warning
         : problems.Count > 0 ? ProblemSeverity.Info
         : null;
}

/// <summary>Versão enxuta (sem body/headers) usada na listagem e no WebSocket.</summary>
public sealed record RequestSummary(
    long Id,
    DateTimeOffset Timestamp,
    string Method,
    string Scheme,
    string Host,
    string Path,
    string QueryString,
    string? RemoteIp,
    string? ContentType,
    string BodyKind,
    long BodySize,
    bool BodyTruncated,
    string[] Problems,
    string? ProblemSeverity);

public static class BodyKinds
{
    public const string None = "none";
    public const string Json = "json";
    public const string Xml = "xml";
    public const string Text = "text";
    public const string Form = "form";
    public const string Multipart = "multipart";
    public const string Binary = "binary";
}

public sealed record BodyCapture
{
    public static readonly BodyCapture Empty = new() { Kind = BodyKinds.None };

    /// <summary>Um de <see cref="BodyKinds"/>.</summary>
    public required string Kind { get; init; }

    /// <summary>Tamanho real do body em bytes (mesmo quando truncado).</summary>
    public long Size { get; init; }

    /// <summary>Verdadeiro quando o body excedeu <c>ECHO_MAX_BODY_BYTES</c> e só parte foi armazenada.</summary>
    public bool Truncated { get; init; }

    /// <summary>Charset usado para decodificar o texto.</summary>
    public string? Charset { get; init; }

    /// <summary>Conteúdo textual (json, xml, texto ou fallback de multipart inválido).</summary>
    public string? Text { get; init; }

    /// <summary>JSON parseado quando válido e completo.</summary>
    public JsonElement? Json { get; init; }

    /// <summary>Motivo pelo qual o JSON não pôde ser parseado.</summary>
    public string? JsonError { get; init; }

    /// <summary>Campos de formulário (form-urlencoded ou multipart).</summary>
    public IReadOnlyDictionary<string, string?[]>? Form { get; init; }

    /// <summary>Metadados dos arquivos de um multipart (o conteúdo não é armazenado).</summary>
    public IReadOnlyList<CapturedFile>? Files { get; init; }

    /// <summary>Conteúdo binário (possivelmente truncado) em base64.</summary>
    public string? Base64 { get; init; }

    /// <summary>Erro ao interpretar o body (ex.: multipart malformado). O request ainda é ecoado.</summary>
    public string? Error { get; init; }
}

public sealed record CapturedFile(string Name, string FileName, string? ContentType, long Length);
