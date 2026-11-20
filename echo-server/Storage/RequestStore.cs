using EchoServer.Capture;

namespace EchoServer.Storage;

public enum StoreEventType { Added, Cleared }

public sealed record StoreEvent(StoreEventType Type, CapturedRequest? Request, long TotalReceived);

/// <summary>
/// Histórico em memória dos últimos N requests (buffer circular).
/// Ao atingir a capacidade, cada novo request sobrescreve o mais antigo.
/// </summary>
public sealed class RequestStore
{
    private readonly Lock gate = new();
    private readonly CapturedRequest?[] buffer;
    private readonly List<Action<StoreEvent>> subscribers = [];
    private int head;   // posição do mais antigo
    private int count;
    private long lastId;

    public RequestStore(EchoOptions options)
    {
        this.Capacity = options.MaxRequests;
        this.buffer = new CapturedRequest?[this.Capacity];
    }

    public int Capacity { get; }

    public int Count { get { lock (this.gate) return this.count; } }

    /// <summary>Total de requests recebidos desde a subida (inclui os já expurgados).</summary>
    public long TotalReceived { get { lock (this.gate) return this.lastId; } }

    /// <summary>Armazena o request atribuindo um Id sequencial e notifica os assinantes.</summary>
    public CapturedRequest Add(CapturedRequest request)
    {
        lock (this.gate)
        {
            var stored = request with { Id = ++this.lastId };
            var index = (this.head + this.count) % this.Capacity;

            if (this.count < this.Capacity)
                this.count++;
            else
                this.head = (this.head + 1) % this.Capacity; // expurga o mais antigo

            this.buffer[index] = stored;

            // Notificação dentro do lock garante a ordem dos eventos; os assinantes só
            // fazem TryWrite em um Channel, então isso nunca bloqueia.
            this.Publish(new StoreEvent(StoreEventType.Added, stored, this.lastId));
            return stored;
        }
    }

    /// <summary>Requests armazenados, do mais recente para o mais antigo.</summary>
    public IReadOnlyList<CapturedRequest> Snapshot(int? limit = null)
    {
        lock (this.gate)
            return this.SnapshotUnsafe(limit);
    }

    public CapturedRequest? Get(long id)
    {
        lock (this.gate)
        {
            if (this.count == 0)
                return null;

            // Ids são contíguos dentro do buffer: acesso O(1).
            var oldestId = this.buffer[this.head]!.Id;
            var offset = id - oldestId;
            if (offset < 0 || offset >= this.count)
                return null;

            return this.buffer[(this.head + (int)offset) % this.Capacity];
        }
    }

    public void Clear()
    {
        lock (this.gate)
        {
            Array.Clear(this.buffer);
            this.head = 0;
            this.count = 0;
            this.Publish(new StoreEvent(StoreEventType.Cleared, null, this.lastId));
        }
    }

    /// <summary>
    /// Registra um assinante e devolve, atomicamente, o snapshot atual: nenhum request
    /// fica de fora nem aparece duplicado entre o snapshot e os eventos seguintes.
    /// </summary>
    public (IReadOnlyList<CapturedRequest> Snapshot, long TotalReceived, IDisposable Subscription) Subscribe(Action<StoreEvent> handler)
    {
        lock (this.gate)
        {
            this.subscribers.Add(handler);
            return (this.SnapshotUnsafe(null), this.lastId, new Unsubscriber(this, handler));
        }
    }

    private IReadOnlyList<CapturedRequest> SnapshotUnsafe(int? limit)
    {
        var take = Math.Clamp(limit ?? this.count, 0, this.count);
        var result = new CapturedRequest[take];
        for (var i = 0; i < take; i++)
            result[i] = this.buffer[(this.head + this.count - 1 - i) % this.Capacity]!;
        return result;
    }

    private void Publish(StoreEvent storeEvent)
    {
        foreach (var subscriber in this.subscribers)
            subscriber(storeEvent);
    }

    private sealed class Unsubscriber(RequestStore store, Action<StoreEvent> handler) : IDisposable
    {
        public void Dispose()
        {
            lock (store.gate)
                store.subscribers.Remove(handler);
        }
    }
}
