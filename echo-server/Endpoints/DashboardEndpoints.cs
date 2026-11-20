using EchoServer.Capture;
using EchoServer.Realtime;
using EchoServer.Storage;

namespace EchoServer.Endpoints;

/// <summary>
/// Rotas reservadas sob <c>/_echo</c>: não são ecoadas nem entram no histórico.
/// </summary>
public static class DashboardEndpoints
{
    public const string Prefix = "/_echo";

    public static bool IsReserved(PathString path) => path.StartsWithSegments(Prefix, StringComparison.OrdinalIgnoreCase);

    public static IEndpointRouteBuilder MapEchoDashboard(this IEndpointRouteBuilder app)
    {
        var group = app.MapGroup(Prefix);

        group.MapGet("/api/requests", (RequestStore store, int? limit) => Results.Json(new
        {
            capacity = store.Capacity,
            count = store.Count,
            totalReceived = store.TotalReceived,
            items = store.Snapshot(limit is > 0 ? limit : null).Select(r => r.ToSummary()),
        }, EchoJson.Api));

        group.MapGet("/api/requests/{id:long}", (RequestStore store, long id)
            => store.Get(id) is { } request ? Results.Json(request, EchoJson.Api) : Results.NotFound());

        group.MapDelete("/api/requests", (RequestStore store) =>
        {
            store.Clear();
            return Results.NoContent();
        });

        group.Map("/ws", RequestStreamWebSocket.HandleAsync);

        return app;
    }
}
