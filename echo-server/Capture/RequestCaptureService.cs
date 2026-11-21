using System.Buffers;
using System.Net;
using System.Text;
using System.Text.Json;
using System.Text.Unicode;
using EchoServer.Storage;
using Microsoft.Extensions.Primitives;
using Microsoft.Net.Http.Headers;
using BadHttpRequestException = Microsoft.AspNetCore.Http.BadHttpRequestException;

namespace EchoServer.Capture;

/// <summary>
/// Converte um <see cref="HttpRequest"/> em <see cref="CapturedRequest"/>.
/// Nunca rejeita o request: falhas de leitura, formato ou codificação viram
/// <see cref="RequestProblem"/> e o que foi possível ler é preservado.
/// </summary>
public sealed class RequestCaptureService(EchoOptions options)
{
    private static readonly HashSet<string> TextualApplicationSubtypes = new(StringComparer.OrdinalIgnoreCase)
    {
        "javascript", "ecmascript", "x-javascript", "x-ndjson", "ndjson", "jsonl", "x-jsonlines",
        "yaml", "x-yaml", "toml", "graphql", "sql", "csv", "x-sh", "x-httpd-php", "rtf",
    };

    private static readonly UTF8Encoding Utf8NoBom = new(encoderShouldEmitUTF8Identifier: false, throwOnInvalidBytes: false);

    public async Task<CapturedRequest> CaptureAsync(HttpContext context, CancellationToken cancellationToken)
    {
        var request = context.Request;
        var problems = new List<RequestProblem>();

        MediaTypeHeaderValue? mediaType = null;
        if (!string.IsNullOrWhiteSpace(request.ContentType) && !MediaTypeHeaderValue.TryParse(request.ContentType, out mediaType))
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.ContentTypeInvalid,
                $"Content-Type '{request.ContentType}' could not be parsed; the body format was inferred from its content."));
        }

        var body = await this.CaptureBodyAsync(request, mediaType, problems, cancellationToken);

        return new CapturedRequest
        {
            Timestamp = DateTimeOffset.UtcNow,
            Method = request.Method,
            Scheme = request.Scheme,
            Host = request.Host.Value ?? string.Empty,
            PathBase = request.PathBase.Value ?? string.Empty,
            Path = request.Path.HasValue ? request.Path.Value! : "/",
            QueryString = request.QueryString.Value ?? string.Empty,
            Query = ToDictionary(request.Query),
            Headers = ToDictionary(request.Headers),
            RemoteIp = FormatIp(context.Connection.RemoteIpAddress),
            Protocol = request.Protocol,
            ContentType = request.ContentType,
            ContentLength = request.ContentLength,
            Body = body,
            Problems = problems,
        };
    }

    // ------------------------------------------------------------------ body

    private async Task<BodyCapture> CaptureBodyAsync(HttpRequest request, MediaTypeHeaderValue? mediaType, List<RequestProblem> problems, CancellationToken cancellationToken)
    {
        if (!request.HasFormContentType)
        {
            var read = await ReadLimitedAsync(request.Body, options.MaxBodyBytes, options.MaxRequestBytes, cancellationToken);
            this.ReportReadOutcome(request, read, problems);
            return this.Interpret(read, mediaType, problems);
        }

        // Bufferiza o body inteiro (memória até 30 KB, depois arquivo temporário) ANTES do parse:
        // assim sabemos o tamanho real e conseguimos reler o conteúdo bruto se algo der errado.
        request.EnableBuffering();
        var drained = await ReadLimitedAsync(request.Body, keepBytes: 0, options.MaxRequestBytes, cancellationToken);
        this.ReportReadOutcome(request, drained, problems);
        request.Body.Position = 0;

        // Body incompleto/excessivo: não dá para parsear o form com segurança. Relê só o que foi
        // bufferizado (limitado a drained.Total para não voltar a tocar no stream da conexão).
        if (drained.Failure is not null || drained.LimitReached)
            return this.Interpret(await ReadLimitedAsync(request.Body, options.MaxBodyBytes, drained.Total, cancellationToken, quiet: true), mediaType, problems);

        try
        {
            var form = await request.ReadFormAsync(cancellationToken);
            var (fields, truncated) = this.CaptureFormFields(form);
            var isMultipart = request.ContentType!.StartsWith("multipart/", StringComparison.OrdinalIgnoreCase);

            if (truncated)
                problems.Add(Truncation(drained.Total));

            return new BodyCapture
            {
                Kind = isMultipart ? BodyKinds.Multipart : BodyKinds.Form,
                Size = drained.Total,
                Truncated = truncated,
                Form = fields,
                Files = isMultipart
                    ? form.Files.Select(f => new CapturedFile(f.Name, f.FileName, f.ContentType, f.Length)).ToArray()
                    : null,
            };
        }
        catch (Exception ex) when (ex is InvalidDataException or IOException or FormatException)
        {
            var message = $"Invalid form: {ex.Message}";
            problems.Add(ProblemCodes.Create(ProblemCodes.FormInvalid, message));

            request.Body.Position = 0;
            var raw = await ReadLimitedAsync(request.Body, options.MaxBodyBytes, drained.Total, cancellationToken, quiet: true);
            return this.Interpret(raw, mediaType, problems) with { Error = message };
        }
    }

    /// <summary>Transforma bytes lidos em <see cref="BodyCapture"/> (json, xml, texto ou binário).</summary>
    private BodyCapture Interpret(ReadResult read, MediaTypeHeaderValue? mediaType, List<RequestProblem> problems)
    {
        if (read.Total == 0)
            return BodyCapture.Empty;

        var data = read.Data;
        var truncated = read.Total > data.Length;
        if (truncated)
            problems.Add(Truncation(read.Total));

        var incomplete = truncated || read.Failure is not null || read.LimitReached;
        var kind = Classify(mediaType, data, incomplete);

        if (kind == BodyKinds.Binary)
            return new BodyCapture { Kind = kind, Size = read.Total, Truncated = truncated, Base64 = Convert.ToBase64String(data) };

        var encoding = this.ResolveEncoding(mediaType, problems);
        if (encoding.CodePage == Encoding.UTF8.CodePage && !IsValidUtf8AllowingCut(data, incomplete))
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.CharsetMismatch,
                "The body contains invalid UTF-8 bytes; invalid characters were replaced with '�'. Check the declared charset."));
        }

        var text = encoding.GetString(data).TrimStart('﻿');
        var capture = new BodyCapture { Kind = kind, Size = read.Total, Truncated = truncated, Charset = encoding.WebName, Text = text };

        if (kind != BodyKinds.Json)
            return capture;

        if (incomplete)
            return capture with { JsonError = $"Body incomplete or truncated ({data.Length} of {read.Total} bytes stored); JSON was not parsed." };

        try
        {
            using var document = JsonDocument.Parse(text);
            return capture with { Json = document.RootElement.Clone() };
        }
        catch (JsonException ex)
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.JsonInvalid, $"Invalid JSON: {ex.Message}"));
            return capture with { JsonError = ex.Message };
        }
    }

    /// <summary>Converte o resultado da leitura do stream em problemas.</summary>
    private void ReportReadOutcome(HttpRequest request, ReadResult read, List<RequestProblem> problems)
    {
        if (read.Failure is not null)
            problems.Add(read.Failure);

        if (read.LimitReached)
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.BodyTooLarge,
                $"The body exceeds ECHO_MAX_REQUEST_BYTES ({options.MaxRequestBytes} bytes); reading stopped and the rest was discarded."));
        }
        else if (read.Failure is null && request.ContentLength is { } declared && declared != read.Total)
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.ContentLengthMismatch,
                $"Content-Length declares {declared} bytes, but {read.Total} were received."));
        }
    }

    private RequestProblem Truncation(long total) => ProblemCodes.Create(ProblemCodes.BodyTruncated,
        $"Body of {total} bytes partially stored (ECHO_MAX_BODY_BYTES = {options.MaxBodyBytes}).");

    private (Dictionary<string, string?[]> Fields, bool Truncated) CaptureFormFields(IFormCollection form)
    {
        var fields = new Dictionary<string, string?[]>(StringComparer.Ordinal);
        var budget = options.MaxBodyBytes;
        var truncated = false;

        foreach (var (key, values) in form)
        {
            var captured = new string?[values.Count];
            for (var i = 0; i < values.Count; i++)
            {
                var value = values[i] ?? string.Empty;
                if (value.Length > budget)
                {
                    value = value[..Math.Max(0, budget)];
                    truncated = true;
                }
                budget -= value.Length;
                captured[i] = value;
            }
            fields[key] = captured;
        }

        return (fields, truncated);
    }

    // ------------------------------------------------------------------ classificação / encoding

    private static string Classify(MediaTypeHeaderValue? mediaType, byte[] data, bool incomplete)
    {
        if (mediaType is null || mediaType.Type.Equals("multipart", StringComparison.OrdinalIgnoreCase))
            return Sniff(data, incomplete);

        var type = mediaType.Type.Value ?? string.Empty;
        var subType = mediaType.SubTypeWithoutSuffix.Value ?? string.Empty;
        var suffix = mediaType.Suffix.Value ?? string.Empty;

        if (Is(subType, "json") || Is(suffix, "json"))
            return BodyKinds.Json;
        if (Is(subType, "xml") || Is(suffix, "xml"))
            return BodyKinds.Xml;
        if (Is(type, "text") || (Is(type, "application") && TextualApplicationSubtypes.Contains(subType)))
            return BodyKinds.Text;

        return BodyKinds.Binary;

        static bool Is(string value, string expected) => value.Equals(expected, StringComparison.OrdinalIgnoreCase);
    }

    /// <summary>Sem Content-Type útil: decide entre json, texto e binário olhando os bytes.</summary>
    private static string Sniff(byte[] data, bool incomplete)
    {
        var span = data.AsSpan();
        if (!IsValidUtf8AllowingCut(span, incomplete) || ContainsControlChars(span))
            return BodyKinds.Binary;

        var first = span.TrimStart(" \t\r\n"u8);
        if (!incomplete && first.Length > 0 && (first[0] == (byte)'{' || first[0] == (byte)'['))
        {
            try
            {
                using var _ = JsonDocument.Parse(data);
                return BodyKinds.Json;
            }
            catch (JsonException) { }
        }

        return BodyKinds.Text;
    }

    // Um corte no meio de um caractere multibyte não deve transformar texto em "binário".
    private static bool IsValidUtf8AllowingCut(ReadOnlySpan<byte> span, bool incomplete)
    {
        if (Utf8.IsValid(span))
            return true;
        if (!incomplete)
            return false;
        for (var cut = 1; cut <= 3 && cut < span.Length; cut++)
        {
            if (Utf8.IsValid(span[..^cut]))
                return true;
        }
        return false;
    }

    private static bool ContainsControlChars(ReadOnlySpan<byte> span)
    {
        foreach (var b in span)
        {
            if (b < 0x20 && b is not ((byte)'\t' or (byte)'\n' or (byte)'\r'))
                return true;
        }
        return false;
    }

    private Encoding ResolveEncoding(MediaTypeHeaderValue? mediaType, List<RequestProblem> problems)
    {
        if (mediaType is null || StringSegment.IsNullOrEmpty(mediaType.Charset))
            return Utf8NoBom;

        var charset = mediaType.Charset.Value!.Trim('"');
        try
        {
            var encoding = Encoding.GetEncoding(charset);
            return encoding.CodePage == Encoding.UTF8.CodePage ? Utf8NoBom : encoding;
        }
        catch (ArgumentException)
        {
            problems.Add(ProblemCodes.Create(ProblemCodes.CharsetUnknown,
                $"Unknown charset '{charset}'; the body was decoded as UTF-8."));
            return Utf8NoBom;
        }
    }

    // ------------------------------------------------------------------ leitura do stream

    /// <param name="Data">Bytes armazenados (no máximo keepBytes).</param>
    /// <param name="Total">Bytes efetivamente recebidos.</param>
    /// <param name="LimitReached">O body passou de maxBytes e a leitura foi interrompida.</param>
    /// <param name="Failure">Motivo pelo qual a leitura terminou antes do fim do body.</param>
    private sealed record ReadResult(byte[] Data, long Total, bool LimitReached, RequestProblem? Failure);

    /// <summary>
    /// Lê o stream (inclusive chunked, sem Content-Length) guardando no máximo <paramref name="keepBytes"/>,
    /// parando após <paramref name="maxBytes"/>. Falhas de conexão não propagam: o que foi lido é devolvido
    /// junto com o problema.
    /// </summary>
    private static async Task<ReadResult> ReadLimitedAsync(Stream body, int keepBytes, long maxBytes, CancellationToken cancellationToken, bool quiet = false)
    {
        using var kept = new MemoryStream();
        var buffer = ArrayPool<byte>.Shared.Rent(16 * 1024);
        long total = 0;
        var limitReached = false;
        RequestProblem? failure = null;

        try
        {
            while (true)
            {
                // Lê até 1 byte além do limite para saber se ele foi realmente excedido. Na releitura
                // (quiet) o limite é exato: passar dele voltaria a ler da conexão, fora do buffer.
                var allowed = (int)Math.Min(buffer.Length, maxBytes + (quiet ? 0 : 1) - total);
                if (allowed <= 0)
                    break;

                var read = await body.ReadAsync(buffer.AsMemory(0, allowed), cancellationToken);
                if (read == 0)
                    break;

                var room = (int)Math.Min(read, Math.Max(0, keepBytes - kept.Length));
                if (room > 0)
                    kept.Write(buffer, 0, room);
                total += read;

                if (total > maxBytes)
                {
                    total = maxBytes;
                    limitReached = !quiet;
                    break;
                }
            }
        }
        catch (BadHttpRequestException ex)
        {
            // Kestrel: Content-Length maior que o enviado, chunked malformado, timeout de leitura...
            failure = ProblemCodes.Create(ProblemCodes.BodyIncomplete, $"Body ended unexpectedly after {total} bytes: {ex.Message}");
        }
        catch (Exception ex) when (ex is OperationCanceledException or IOException)
        {
            failure = ProblemCodes.Create(ProblemCodes.ClientAborted, $"The client closed the connection after sending {total} bytes of the body.");
        }
        finally
        {
            ArrayPool<byte>.Shared.Return(buffer);
        }

        return new ReadResult(kept.ToArray(), total, limitReached, quiet ? null : failure);
    }

    // ------------------------------------------------------------------ utilitários

    private static Dictionary<string, string?[]> ToDictionary(IEnumerable<KeyValuePair<string, StringValues>> source)
    {
        var result = new Dictionary<string, string?[]>(StringComparer.OrdinalIgnoreCase);
        foreach (var (key, values) in source)
            result[key] = values.ToArray();
        return result;
    }

    private static string? FormatIp(IPAddress? address)
        => address is null ? null : (address.IsIPv4MappedToIPv6 ? address.MapToIPv4() : address).ToString();
}
