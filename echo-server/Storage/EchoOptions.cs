using System.Globalization;

namespace EchoServer.Storage;

/// <summary>
/// Configurações lidas das variáveis de ambiente na subida do container.
/// </summary>
public sealed class EchoOptions
{
    public const int DefaultMaxRequests = 1000;
    public const int DefaultMaxBodyBytes = 64 * 1024;
    public const int DefaultMaxRequestBytes = 30_000_000; // mesmo default do Kestrel

    /// <summary>Quantidade de requests mantidos em memória (env <c>ECHO_MAX_REQUESTS</c>).</summary>
    public int MaxRequests { get; init; } = DefaultMaxRequests;

    /// <summary>Bytes de body armazenados por request; o excedente é truncado (env <c>ECHO_MAX_BODY_BYTES</c>).</summary>
    public int MaxBodyBytes { get; init; } = DefaultMaxBodyBytes;

    /// <summary>
    /// Bytes lidos de um body (env <c>ECHO_MAX_REQUEST_BYTES</c>). Acima disso o echo para de ler,
    /// mas ainda responde e sinaliza <c>body_too_large</c> em vez de rejeitar com 413.
    /// </summary>
    public int MaxRequestBytes { get; init; } = DefaultMaxRequestBytes;

    /// <summary>Nome exibido no echo e no painel (env <c>APP_NAME</c>).</summary>
    public string AppName { get; init; } = "Echo";

    /// <summary>Cor Tailwind do cabeçalho (env <c>HEADER_COLOR</c>).</summary>
    public string HeaderColor { get; init; } = "gray-900";

    /// <summary>
    /// Inclui as variáveis de ambiente do processo no echo (JSON e HTML) (env <c>ECHO_SHOW_ENVIRONMENT</c>).
    /// Default <c>true</c> por compatibilidade; desligue em ambientes onde elas podem conter segredos.
    /// </summary>
    public bool ShowEnvironment { get; init; } = true;

    public static EchoOptions FromConfiguration(IConfiguration configuration) => new()
    {
        ShowEnvironment = ReadBool(configuration, "ECHO_SHOW_ENVIRONMENT", fallback: true),
        MaxRequests = ReadPositiveInt(configuration, "ECHO_MAX_REQUESTS", DefaultMaxRequests),
        MaxBodyBytes = ReadPositiveInt(configuration, "ECHO_MAX_BODY_BYTES", DefaultMaxBodyBytes),
        MaxRequestBytes = ReadPositiveInt(configuration, "ECHO_MAX_REQUEST_BYTES", DefaultMaxRequestBytes),
        AppName = ReadString(configuration, "APP_NAME", "Echo"),
        HeaderColor = ReadString(configuration, "HEADER_COLOR", "gray-900"),
    };

    private static string ReadString(IConfiguration configuration, string key, string fallback)
    {
        var value = configuration[key];
        return string.IsNullOrWhiteSpace(value) ? fallback : value.Trim();
    }

    private static readonly string[] TrueValues = ["true", "1", "yes", "y", "on", "sim", "s"];
    private static readonly string[] FalseValues = ["false", "0", "no", "n", "off", "nao", "não"];

    private static bool ReadBool(IConfiguration configuration, string key, bool fallback)
    {
        var raw = configuration[key];
        if (string.IsNullOrWhiteSpace(raw))
            return fallback;

        var value = raw.Trim();
        if (TrueValues.Contains(value, StringComparer.OrdinalIgnoreCase))
            return true;
        if (FalseValues.Contains(value, StringComparer.OrdinalIgnoreCase))
            return false;

        throw new InvalidOperationException($"The environment variable {key} must be true or false, but got '{raw}'.");
    }

    // Valor inválido derruba a subida com mensagem clara (fail fast) em vez de assumir o default silenciosamente.
    private static int ReadPositiveInt(IConfiguration configuration, string key, int fallback)
    {
        var raw = configuration[key];
        if (string.IsNullOrWhiteSpace(raw))
            return fallback;

        if (int.TryParse(raw.Trim(), NumberStyles.Integer, CultureInfo.InvariantCulture, out var value) && value > 0)
            return value;

        throw new InvalidOperationException($"The environment variable {key} must be a positive integer, but got '{raw}'.");
    }
}
