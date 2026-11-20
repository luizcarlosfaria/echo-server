namespace EchoServer.Capture;

/// <summary>
/// Algo de errado (ou digno de nota) no request recebido. O echo nunca rejeita o request
/// por isso: ele responde normalmente e sinaliza o problema no JSON, no header
/// <c>X-Echo-Problems</c>, na página HTML e no painel.
/// </summary>
public sealed record RequestProblem(string Code, string Severity, string Message);

public static class ProblemSeverity
{
    /// <summary>O request está malformado ou chegou incompleto.</summary>
    public const string Error = "error";

    /// <summary>O request foi interpretado, mas com suposições (ex.: charset desconhecido).</summary>
    public const string Warning = "warning";

    /// <summary>Limitação do próprio echo ao armazenar o request (ex.: body truncado).</summary>
    public const string Info = "info";
}

public static class ProblemCodes
{
    public const string JsonInvalid = "json_invalid";
    public const string FormInvalid = "form_invalid";
    public const string BodyIncomplete = "body_incomplete";
    public const string ClientAborted = "client_aborted";
    public const string ContentLengthMismatch = "content_length_mismatch";
    public const string BodyTooLarge = "body_too_large";
    public const string ContentTypeInvalid = "content_type_invalid";
    public const string CharsetUnknown = "charset_unknown";
    public const string CharsetMismatch = "charset_mismatch";
    public const string BodyTruncated = "body_truncated";

    public static RequestProblem Create(string code, string message) => new(code, SeverityOf(code), message);

    private static string SeverityOf(string code) => code switch
    {
        JsonInvalid or FormInvalid or BodyIncomplete or ClientAborted or ContentLengthMismatch => ProblemSeverity.Error,
        BodyTooLarge or ContentTypeInvalid or CharsetUnknown or CharsetMismatch => ProblemSeverity.Warning,
        _ => ProblemSeverity.Info,
    };
}
