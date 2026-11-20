using EchoServer.Capture;
using EchoServer.Storage;

namespace EchoServer.Models;

public sealed record EchoViewModel(CapturedRequest Request, EchoOptions Options, IReadOnlyDictionary<string, string?> EnvironmentVariables);

public sealed record DashboardViewModel(EchoOptions Options, int Capacity);
