using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using EchoServer.IntegrationTests.Infrastructure;

namespace EchoServer.IntegrationTests;

/// <summary>
/// Garante que os formatos de body mais comuns são aceitos e devolvidos corretamente.
/// </summary>
public class EchoBodyFormatTests(EchoAppFactory factory) : IClassFixture<EchoAppFactory>
{
    private readonly HttpClient client = factory.CreateClient();

    private static StringContent Text(string body, string mediaType, string? charset = "utf-8")
    {
        var content = new StringContent(body, Encoding.UTF8);
        content.Headers.ContentType = new MediaTypeHeaderValue(mediaType) { CharSet = charset };
        return content;
    }

    [Theory]
    [InlineData("POST")]
    [InlineData("PUT")]
    [InlineData("PATCH")]
    [InlineData("DELETE")]
    public async Task Json_object_is_parsed_for_any_verb_with_body(string verb)
    {
        var echo = await this.client.EchoAsync(new HttpMethod(verb), "/json", Text("""{"id":1,"nome":"Ana"}""", "application/json", charset: null));

        Assert.Equal("json", echo.Body().Str("Kind"));
        Assert.Equal(1, echo.GetProperty("Payload").GetProperty("id").GetInt32());
        Assert.Equal("Ana", echo.GetProperty("Payload").Str("nome"));
    }

    [Fact]
    public async Task Json_with_charset_parameter_is_parsed()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/json", Text("""{"acentuação":"é"}""", "application/json"));

        Assert.Equal("é", echo.GetProperty("Payload").Str("acentuação"));
    }

    [Theory]
    [InlineData("""[1,2,3]""", JsonValueKind.Array)]
    [InlineData("""42""", JsonValueKind.Number)]
    [InlineData("""  "texto"  """, JsonValueKind.String)]
    [InlineData("""null""", JsonValueKind.Null)]
    public async Task Json_non_object_roots_are_parsed(string json, JsonValueKind expected)
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/json", Text(json, "application/json"));

        Assert.Equal("json", echo.Body().Str("Kind"));
        Assert.Equal(expected, echo.GetProperty("Payload").ValueKind);
    }

    [Theory]
    [InlineData("application/problem+json")]
    [InlineData("application/vnd.api+json")]
    [InlineData("text/json")]
    public async Task Json_media_type_variants_are_parsed(string mediaType)
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/json", Text("""{"ok":true}""", mediaType));

        Assert.True(echo.GetProperty("Payload").GetProperty("ok").GetBoolean());
    }

    [Fact]
    public async Task Invalid_json_does_not_fail_and_reports_error()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/json", Text("{ isto não é json", "application/json"));

        var body = echo.Body();
        Assert.Equal("json", body.Str("Kind"));
        Assert.Equal("{ isto não é json", body.Str("Text"));
        Assert.False(string.IsNullOrWhiteSpace(body.Str("JsonError")));
        Assert.False(echo.TryGetProperty("Payload", out _));
    }

    [Theory]
    [InlineData("application/xml")]
    [InlineData("text/xml")]
    [InlineData("application/soap+xml")]
    public async Task Xml_is_echoed_as_text(string mediaType)
    {
        const string xml = """<?xml version="1.0"?><pedido id="7"><item>Café</item></pedido>""";

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/xml", Text(xml, mediaType));

        Assert.Equal("xml", echo.Body().Str("Kind"));
        Assert.Equal(xml, echo.Body().Str("Text"));
    }

    [Theory]
    [InlineData("text/plain")]
    [InlineData("text/csv")]
    [InlineData("application/x-ndjson")]
    [InlineData("application/javascript")]
    public async Task Plain_text_formats_are_echoed(string mediaType)
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/texto", Text("linha 1\nlinha 2 ç", mediaType));

        Assert.Equal("text", echo.Body().Str("Kind"));
        Assert.Equal("linha 1\nlinha 2 ç", echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Text_in_latin1_charset_is_decoded()
    {
        var content = new ByteArrayContent(Encoding.Latin1.GetBytes("ação"));
        content.Headers.ContentType = new MediaTypeHeaderValue("text/plain") { CharSet = "iso-8859-1" };

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/latin1", content);

        Assert.Equal("ação", echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Url_encoded_form_is_echoed()
    {
        var content = new FormUrlEncodedContent([new("nome", "Ana Maria"), new("tag", "a"), new("tag", "b")]);

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/form", content);

        Assert.Equal("form", echo.Body().Str("Kind"));
        Assert.Equal("nome=Ana+Maria&tag=a&tag=b".Length, echo.Body().GetProperty("Size").GetInt64());
        var form = echo.Body().GetProperty("Form");
        Assert.Equal("Ana Maria", form.GetProperty("nome")[0].GetString());
        Assert.Equal(2, form.GetProperty("tag").GetArrayLength());

        // formato legado: array de { Key, Value }
        Assert.Contains(echo.GetProperty("Forms").EnumerateArray(), it => it.Str("Key") == "nome");
    }

    [Fact]
    public async Task Multipart_with_fields_and_files_is_echoed()
    {
        var fileBytes = new byte[5000];
        Random.Shared.NextBytes(fileBytes);

        var content = new MultipartFormDataContent
        {
            { new StringContent("valor do campo"), "campo" },
            { new ByteArrayContent(fileBytes) { Headers = { ContentType = new("image/png") } }, "arquivo", "foto.png" },
            { new StringContent("conteúdo txt"), "doc", "nota.txt" },
        };

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/upload", content);

        var body = echo.Body();
        Assert.Equal("multipart", body.Str("Kind"));
        Assert.True(body.GetProperty("Size").GetInt64() > 5000, "Size deve refletir o body inteiro (campos + arquivos)");
        Assert.Equal("valor do campo", body.GetProperty("Form").GetProperty("campo")[0].GetString());

        var files = body.GetProperty("Files").EnumerateArray().ToList();
        Assert.Equal(2, files.Count);
        var foto = files.Single(f => f.Str("FileName") == "foto.png");
        Assert.Equal("arquivo", foto.Str("Name"));
        Assert.Equal("image/png", foto.Str("ContentType"));
        Assert.Equal(5000, foto.GetProperty("Length").GetInt64());
    }

    [Fact]
    public async Task Malformed_multipart_does_not_fail()
    {
        var content = new StringContent("isto não é multipart");
        content.Headers.ContentType = MediaTypeHeaderValue.Parse("multipart/form-data; boundary=----nada");

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/upload-quebrado", content);

        Assert.False(string.IsNullOrWhiteSpace(echo.Body().Str("Error")));
        Assert.Equal("isto não é multipart", echo.Body().Str("Text"));
    }

    [Fact]
    public async Task Binary_body_is_echoed_as_base64()
    {
        byte[] bytes = [0x00, 0xFF, 0x10, 0x80, 0x7F];
        var content = new ByteArrayContent(bytes);
        content.Headers.ContentType = new MediaTypeHeaderValue("application/octet-stream");

        var echo = await this.client.EchoAsync(HttpMethod.Put, "/binario", content);

        Assert.Equal("binary", echo.Body().Str("Kind"));
        Assert.Equal(5, echo.Body().GetProperty("Size").GetInt64());
        Assert.Equal(bytes, Convert.FromBase64String(echo.Body().Str("Base64")));
    }

    [Fact]
    public async Task Body_without_content_type_is_detected()
    {
        var content = new ByteArrayContent(Encoding.UTF8.GetBytes("""{"semTipo":true}"""));

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/sem-content-type", content);

        Assert.Equal("json", echo.Body().Str("Kind"));
        Assert.True(echo.GetProperty("Payload").GetProperty("semTipo").GetBoolean());
    }

    [Fact]
    public async Task Empty_body_with_content_type_is_accepted()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Post, "/vazio", Text("", "application/json"));

        Assert.Equal("none", echo.Body().Str("Kind"));
        Assert.Equal(0, echo.Body().GetProperty("Size").GetInt64());
    }

    [Fact]
    public async Task Chunked_body_without_content_length_is_fully_read()
    {
        var payload = "{\"itens\":[" + string.Join(',', Enumerable.Range(0, 2000)) + "]}";
        var content = new StreamContent(new NonSeekableStream(Encoding.UTF8.GetBytes(payload)));
        content.Headers.ContentType = new MediaTypeHeaderValue("application/json");

        var echo = await this.client.EchoAsync(HttpMethod.Post, "/chunked", content);

        Assert.Equal(2000, echo.GetProperty("Payload").GetProperty("itens").GetArrayLength());
        Assert.Equal(Encoding.UTF8.GetByteCount(payload), echo.Body().GetProperty("Size").GetInt64());
    }

    [Fact]
    public async Task Get_with_body_is_echoed()
    {
        var echo = await this.client.EchoAsync(HttpMethod.Get, "/get-com-body", Text("""{"filtro":"x"}""", "application/json"));

        Assert.Equal("x", echo.GetProperty("Payload").Str("filtro"));
    }

    /// <summary>Stream sem tamanho conhecido: força o HttpClient a não enviar Content-Length.</summary>
    private sealed class NonSeekableStream(byte[] data) : MemoryStream(data)
    {
        public override bool CanSeek => false;
        public override long Length => throw new NotSupportedException();
        public override long Position { get => throw new NotSupportedException(); set => throw new NotSupportedException(); }
    }
}
