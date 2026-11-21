using EchoServer.Capture;
using EchoServer.Endpoints;
using EchoServer.Storage;
using Microsoft.AspNetCore.HttpOverrides;

var builder = WebApplication.CreateBuilder(args);

// Variáveis de ambiente (ECHO_MAX_REQUESTS, ECHO_MAX_BODY_BYTES, APP_NAME, HEADER_COLOR)
// chegam via IConfiguration, que já inclui o ambiente por padrão.
var echoOptions = EchoOptions.FromConfiguration(builder.Configuration);

builder.Services.AddSingleton(echoOptions);

// O limite de body passa a ser do echo (ECHO_MAX_REQUEST_BYTES): em vez de o Kestrel responder 413
// sem que o request chegue à aplicação, o echo lê até o limite, responde e sinaliza body_too_large.
builder.WebHost.ConfigureKestrel(kestrel => kestrel.Limits.MaxRequestBodySize = null);
builder.Services.AddSingleton<RequestStore>();
builder.Services.AddSingleton<RequestCaptureService>();
builder.Services.AddControllersWithViews();
builder.Services.AddProblemDetails();

// Echo é usado atrás de proxies/ingress/API gateways: confia em X-Forwarded-* de qualquer origem.
builder.Services.Configure<ForwardedHeadersOptions>(options =>
{
    options.ForwardedHeaders = ForwardedHeaders.All;
    options.KnownIPNetworks.Clear();
    options.KnownProxies.Clear();
});

var app = builder.Build();

app.UseForwardedHeaders(); // primeiro: tudo que vem depois enxerga Scheme/Host/IP originais

if (app.Environment.IsDevelopment())
    app.UseDeveloperExceptionPage();
else
    app.UseExceptionHandler();

app.UseWebSockets(new WebSocketOptions { KeepAliveInterval = TimeSpan.FromSeconds(30) });
app.UseStaticFiles();

// Explícito e DEPOIS dos estáticos: sem isso o WebApplication põe o UseRouting no início, o
// catch-all {**path} é escolhido para /_echo/dashboard.js e o StaticFileMiddleware o ignora.
app.UseRouting();

app.MapEchoDashboard();
app.MapControllers();
app.MapControllerRoute(
    name: "echo",
    pattern: "{**path}",
    defaults: new { controller = "Echo", action = "Index" });

app.Logger.LogInformation(
    "Echo '{AppName}' keeping up to {MaxRequests} requests (body up to {MaxBodyBytes} bytes). Dashboard at /_echo",
    echoOptions.AppName, echoOptions.MaxRequests, echoOptions.MaxBodyBytes);

app.Run();

public partial class Program;
