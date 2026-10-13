using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace Licentivo;

public sealed partial class LicentivoClient
{
    private readonly SemaphoreSlim featureTaskMutex = new(1, 1);
    private sealed record FeatureTask(int Version, string OperationId, string ReservationId, string State, string Feature, long Quantity);
    private string TaskPath(string id)
    {
        if (string.IsNullOrEmpty(id) || id.Length > 80 || id.Any(ch => ch < 33 || ch > 126))
            throw new ArgumentException("a stable operationID of 1-80 non-whitespace ASCII characters is required");
        return config.CachePath + ".task-" + Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(CacheHash + "\n" + id))).ToLowerInvariant() + ".json";
    }
    private static FeatureTask ReadTask(string path, string id)
    {
        if (!File.Exists(path)) return new(1, id, "", "", "", 0);
        if (new FileInfo(path).Length > 4096) throw new IOException("invalid feature task journal");
        var task = JsonSerializer.Deserialize<FeatureTask>(File.ReadAllText(path));
        if (task is null || task.Version != 1 || task.OperationId != id || string.IsNullOrEmpty(task.Feature) || task.Quantity <= 0 || string.IsNullOrEmpty(task.ReservationId) || task.State is null) throw new IOException("invalid feature task journal; do not repeat business");
        return task;
    }
    private static void SaveTask(string path, FeatureTask task)
    {
        path = Path.GetFullPath(path);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        var temporary = path + "." + Guid.NewGuid() + ".tmp";
        try
        {
            using (var stream = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            {
                if (!OperatingSystem.IsWindows()) File.SetUnixFileMode(temporary, UnixFileMode.UserRead | UnixFileMode.UserWrite);
                JsonSerializer.Serialize(stream, task);
                stream.Flush(true);
            }
            File.Move(temporary, path, true);
        }
        finally { File.Delete(temporary); }
    }
    private async Task FinishTask(string path, FeatureTask task, bool succeeded, CancellationToken ct)
    {
        Reservation hold;
        try { hold = succeeded ? await Commit(task.ReservationId, task.OperationId, ct) : await Cancel(task.ReservationId, task.OperationId, ct); }
        catch (Exception error) { throw new IOException($"confirmation pending for operation {task.OperationId}; do not repeat business", error); }
        var expected = succeeded ? "committed" : "canceled";
        if (hold.Status != expected) throw new InvalidOperationException("reservation closed without requested outcome; do not repeat business");
        SaveTask(path, task with { State = expected });
    }
    /// <summary>Reserve, run the business callback, then commit/cancel. A pending commit never reruns business.</summary>
    public async Task RunMeteredFeature(string feature, long quantity, string operationID, Func<Task> action, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(action);
        if (quantity <= 0) throw new ArgumentException("positive quantity is required");
        if (!featureTaskMutex.Wait(0)) throw new InvalidOperationException("another feature task is running");
        try
        {
            var path = TaskPath(operationID);
            var task = ReadTask(path, operationID);
            if (task.State != "" && (task.Feature != feature || task.Quantity != quantity)) throw new InvalidOperationException("operation ID was already used for another feature or quantity");
            task = task with { Feature = feature, Quantity = quantity };
            if (task.State == "committed") return;
            if (task.State == "commit_pending") { await FinishTask(path, task, true, ct); return; }
            if (task.State == "cancel_pending") { await FinishTask(path, task, false, ct); task = task with { State = "canceled" }; }
            if (task.State == "canceled") throw new InvalidOperationException("operation canceled; use a new job ID for a new action");
            if (task.State == "business_running") throw new InvalidOperationException("business outcome unknown; verify its result and call ResolveMeteredFeature");
            if (task.State != "") throw new InvalidOperationException("invalid feature task state");
            await RunFeature(feature, () => Task.CompletedTask);
            var hold = await Reserve(feature, quantity, operationID, ct);
            if (hold.Status == "committed") { SaveTask(path, task with { ReservationId = hold.ReservationId, State = "committed" }); return; }
            if (hold.Status != "pending") throw new InvalidOperationException("reservation is closed; business was not run");
            task = task with { ReservationId = hold.ReservationId, State = "business_running" };
            try { SaveTask(path, task); }
            catch { try { await Cancel(hold.ReservationId, operationID, ct); } catch { } throw; }
            try { await action(); }
            catch
            {
                task = task with { State = "cancel_pending" };
                try { SaveTask(path, task); await FinishTask(path, task, false, ct); } catch { }
                throw;
            }
            task = task with { State = "commit_pending" };
            try { SaveTask(path, task); }
            catch (Exception error) { throw new IOException($"business completed; journal update failed; do not repeat operation {operationID}", error); }
            await FinishTask(path, task, true, ct);
        }
        finally { featureTaskMutex.Release(); }
    }
    /// <summary>Settle the recorded task only. For a crashed callback, inspect your durable business result first.</summary>
    public async Task ResolveMeteredFeature(string operationID, bool succeeded, CancellationToken ct = default)
    {
        if (!featureTaskMutex.Wait(0)) throw new InvalidOperationException("another feature task is running");
        try
        {
            var path = TaskPath(operationID);
            var task = ReadTask(path, operationID);
            if (task.State == (succeeded ? "committed" : "canceled")) return;
            if (task.ReservationId == "" || task.State is not ("business_running" or "commit_pending" or "cancel_pending") || task.State == "commit_pending" && !succeeded || task.State == "cancel_pending" && succeeded)
                throw new InvalidOperationException("task cannot be resolved with that outcome");
            task = task with { State = succeeded ? "commit_pending" : "cancel_pending" };
            SaveTask(path, task);
            await FinishTask(path, task, succeeded, ct);
        }
        finally { featureTaskMutex.Release(); }
    }
}
