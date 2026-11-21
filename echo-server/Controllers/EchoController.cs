using System.Globalization;
using EchoServer.Capture;
using EchoServer.Endpoints;
using EchoServer.Models;
using EchoServer.Storage;
using Microsoft.AspNetCore.Mvc;

namespace EchoServer.Controllers;

/// <summary>
/// Catch-all: responde a qualquer verbo e qualquer path (exceto <c>/_echo/*</c>)
/// devolvendo o que recebeu, em HTML para navegadores e JSON para o resto.
/// </summary>
public sealed class EchoController(RequestCaptureService capture, RequestStore store, EchoOptions options) : Controller
{
    // Sem parâmetros de propósito: qualquer parâmetro faz o MVC criar os value providers, e o
    // FormValueProviderFactory lê o form antecipadamente, consumindo o body antes da captura.
    public async Task<IActionResult> Index()
    {
        if (DashboardEndpoints.IsReserved(this.Request.Path))
            return this.NotFound();

        // CancellationToken.None de propósito: com o RequestAborted, um cliente que fecha a conexão (FIN/RST)
        // cancelaria a leitura imediatamente e perderíamos os bytes que JÁ chegaram. Quedas de conexão
        // continuam aparecendo como exceção na leitura, e o Kestrel corta clientes lentos demais
        // (MinRequestBodyDataRate), então a leitura nunca fica pendurada.
        var captured = store.Add(await capture.CaptureAsync(this.HttpContext, CancellationToken.None));

        // Cliente já foi embora: o request fica no histórico (com client_aborted), mas não há para quem responder.
        if (this.HttpContext.RequestAborted.IsCancellationRequested)
            return new EmptyResult();

        this.Response.Headers["X-Echo-Request-Id"] = captured.Id.ToString(CultureInfo.InvariantCulture);
        if (captured.Problems.Count > 0)
            this.Response.Headers["X-Echo-Problems"] = string.Join(", ", captured.Problems.Select(p => p.Code).Distinct());

        if (HttpMethods.IsHead(this.Request.Method))
        {
            // HEAD: mesmos headers do GET, sem corpo (TestServer e alguns proxies não descartam sozinhos).
            this.Response.ContentType = "application/json; charset=utf-8";
            return this.Ok();
        }

        if (EchoResponse.WantsHtml(this.Request))
            return this.View("Index", new EchoViewModel(captured, options, options.ShowEnvironment ? EnvironmentSnapshot.Get() : null));

        var json = EchoResponse.Build(captured, options).ToJsonString(EchoJson.Echo);
        return this.Content(json, "application/json; charset=utf-8");
    }
}

public sealed class DashboardController(EchoOptions options, RequestStore store) : Controller
{
    [HttpGet("/_echo")]
    public IActionResult Index() => this.View(new DashboardViewModel(options, store.Capacity));
}
