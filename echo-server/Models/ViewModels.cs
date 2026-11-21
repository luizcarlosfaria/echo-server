using EchoServer.Capture;
using EchoServer.Storage;

namespace EchoServer.Models;

/// <param name="EnvironmentVariables">Nulo quando ECHO_SHOW_ENVIRONMENT=false.</param>
public sealed record EchoViewModel(CapturedRequest Request, EchoOptions Options, IReadOnlyDictionary<string, string?>? EnvironmentVariables);

public sealed record DashboardViewModel(EchoOptions Options, int Capacity);
