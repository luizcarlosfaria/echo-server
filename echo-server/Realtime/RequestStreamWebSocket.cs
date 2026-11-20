using System.Net.WebSockets;
using System.Text.Json;
using System.Threading.Channels;
using EchoServer.Capture;
using EchoServer.Storage;

namespace EchoServer.Realtime;

/// <summary>
/// WebSocket em <c>/_echo/ws</c>. Ao conectar envia um snapshot (resumos) e depois um
/// evento por request recebido. Cada conexão tem seu próprio Channel limitado: um cliente
/// lento perde eventos antigos (DropOldest) mas nunca atrasa o echo nem os outros clientes.
/// </summary>
public static class RequestStreamWebSocket
{
    private const int PerClientQueue = 512;

    public static async Task HandleAsync(HttpContext context, RequestStore store, IHostApplicationLifetime lifetime, ILogger<RequestStore> logger)
    {
        if (!context.WebSockets.IsWebSocketRequest)
        {
            context.Response.StatusCode = StatusCodes.Status400BadRequest;
            await context.Response.WriteAsync("Este endpoint aceita apenas conexões WebSocket.", context.RequestAborted);
            return;
        }

        using var socket = await context.WebSockets.AcceptWebSocketAsync();
        var queue = Channel.CreateBounded<StoreEvent>(new BoundedChannelOptions(PerClientQueue)
        {
            FullMode = BoundedChannelFullMode.DropOldest,
            SingleReader = true,
        });

        var (snapshot, totalReceived, subscription) = store.Subscribe(e => queue.Writer.TryWrite(e));
        using var _ = subscription;
        using var stop = CancellationTokenSource.CreateLinkedTokenSource(context.RequestAborted, lifetime.ApplicationStopping);
        var receiving = ReceiveUntilClosedAsync(socket, stop, context.RequestAborted);

        try
        {
            await SendAsync(socket, new
            {
                type = "snapshot",
                capacity = store.Capacity,
                totalReceived,
                items = snapshot.Select(r => r.ToSummary()),
            }, stop.Token);

            await foreach (var storeEvent in queue.Reader.ReadAllAsync(stop.Token))
            {
                object message = storeEvent.Type switch
                {
                    StoreEventType.Added => new { type = "request", totalReceived = storeEvent.TotalReceived, item = storeEvent.Request!.ToSummary() },
                    _ => new { type = "cleared", totalReceived = storeEvent.TotalReceived },
                };
                await SendAsync(socket, message, stop.Token);
            }
        }
        catch (OperationCanceledException) { }
        catch (Exception ex) when (ex is WebSocketException or IOException or ObjectDisposedException)
        {
            logger.LogDebug(ex, "Conexão WebSocket do painel encerrada abruptamente.");
        }
        finally
        {
            // A conexão já pode ter caído de qualquer jeito: fechar é best-effort e nunca deve
            // propagar exceção (a resposta já começou, não há o que o pipeline possa fazer).
            if (socket.State is WebSocketState.Open or WebSocketState.CloseReceived)
            {
                try
                {
                    using var closeTimeout = new CancellationTokenSource(TimeSpan.FromSeconds(2));
                    await socket.CloseOutputAsync(WebSocketCloseStatus.NormalClosure, "bye", closeTimeout.Token);
                }
                catch (Exception ex)
                {
                    logger.LogDebug(ex, "Falha ao fechar WebSocket do painel.");
                }
            }
            await Task.WhenAny(receiving, Task.Delay(TimeSpan.FromSeconds(2)));
        }
    }

    /// <summary>O cliente não envia nada útil; lemos apenas para detectar o fechamento.</summary>
    private static async Task ReceiveUntilClosedAsync(WebSocket socket, CancellationTokenSource stop, CancellationToken aborted)
    {
        var buffer = new byte[1024];
        try
        {
            while (socket.State is WebSocketState.Open or WebSocketState.CloseSent)
            {
                var result = await socket.ReceiveAsync(buffer, aborted);
                if (result.MessageType == WebSocketMessageType.Close)
                    break;
            }
        }
        catch (Exception ex) when (ex is WebSocketException or OperationCanceledException or IOException or ObjectDisposedException) { }
        finally
        {
            try { stop.Cancel(); }
            catch (ObjectDisposedException) { } // o loop de envio já terminou e descartou o token
        }
    }

    private static Task SendAsync(WebSocket socket, object message, CancellationToken cancellationToken)
    {
        var bytes = JsonSerializer.SerializeToUtf8Bytes(message, EchoJson.Api);
        return socket.SendAsync(bytes, WebSocketMessageType.Text, endOfMessage: true, cancellationToken);
    }
}
