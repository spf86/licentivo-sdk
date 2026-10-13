namespace Licentivo;

public sealed partial class LicentivoClient
{
    // Non-metered entitlement check. The callback is never invoked when denied.
    public async Task RunFeature(string feature, Func<Task> action)
    {
        ArgumentNullException.ThrowIfNull(action);
        var status = FeatureStatus(feature);
        if (!status.Allowed)
            throw new InvalidOperationException($"feature denied: {status.Code} ({status.State}); request_id={status.RequestID}");
        await action();
    }
}
