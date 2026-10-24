using System.Diagnostics;
using System.Text.Json;
namespace Licentivo;

public sealed record AuthorizationStatus(string State,bool Allowed,string Code="",string RequestID="",bool Retryable=false,long LeaseValidUntil=0);
public sealed record Consumption(string Feature,string Period,long Quantity,long Used,long Limit,long Remaining,long Overage,long Reserved,DateTimeOffset? ResetAt);
public sealed record Reservation(string ReservationId,string OperationId,string Status,DateTimeOffset ExpiresAt,Consumption Consumption);
public sealed partial class LicentivoClient {
    private sealed record Snapshot(JsonElement? Lease,long Wall,long Tick);
    private volatile Snapshot? snapshot;
    private volatile bool online;
    private volatile Exception? lastError;
    private DateTimeOffset retryAt;
    private Task? refreshTask;
    private readonly CancellationTokenSource startupCts=new();
    public event Action<AuthorizationStatus>? StatusChanged;
    private Claims LocalCheck(){if(signedOut)throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");var s=snapshot;if(s?.Lease is null)throw new InvalidOperationException("verification required");if(s.Wall>0&&Now<s.Wall+Stopwatch.GetElapsedTime(s.Tick).TotalSeconds-300)throw new InvalidOperationException("clock rollback");var c=Verify(s.Lease.Value,config.TrustedKeys,config.ProductId,config.DeviceId,Now);CheckContext(c);return c;}
    private void Publish(bool currentOnline,Exception? failure){var before=Status();snapshot=new(cached,wall,tick);online=currentOnline;lastError=failure;if(failure is ApiException e&&e.RetryAfterSeconds>0)retryAt=DateTimeOffset.UtcNow.AddSeconds(e.RetryAfterSeconds);var after=Status();var callback=StatusChanged;if(before!=after&&callback is not null)_=Task.Run(()=>callback(after));}
    public AuthorizationStatus Status(){if(signedOut)return new("released",false,"LAUNCH_LOGIN_REQUIRED");string state="verification_required";bool allowed=false;long expires=0;try{var c=LocalCheck();allowed=true;expires=c.ExpiresAt;state=online?"active":"offline_valid";}catch{}var error=lastError as ApiException;string code=error?.Code??(lastError is null?"":"NETWORK_UNAVAILABLE");state=code switch{"LICENSE_EXPIRED"=>"expired","LICENSE_REVOKED" or "LICENSE_SUSPENDED"=>"revoked","DEVICE_RELEASED" or "DEVICE_CREDENTIAL_REVOKED"=>"released","FEATURE_QUOTA_EXCEEDED"=>"quota_exhausted",_=>state};return new(state,allowed,code,error?.RequestID??"",error?.Retryable??lastError is not null,expires);}
    public AuthorizationStatus FeatureStatus(string name){var s=Status();if(!s.Allowed)return s;try{return LocalCheck().Features.Contains(name)?s:s with{Allowed=false,Code="FEATURE_NOT_ENTITLED"};}catch{return s with{Allowed=false,State="verification_required"};}}
    private async Task<Reservation> ReservationOperation(string op,string operationID,Dictionary<string,object> extra,CancellationToken ct){if(string.IsNullOrEmpty(operationID))throw new ArgumentException("retain operationID for retries");await mutex.WaitAsync(ct);try{var c=Check();try{var data=await Request("consume/"+op,c.ActivationId,ct,operationID,extra);var result=data.GetProperty("reservation").Deserialize<Reservation>(JsonOptions)??throw new JsonException("reservation missing");Publish(true,null);return result;}catch(Exception e){if(e is ApiException a&&new[]{"LICENSE_REVOKED","LICENSE_SUSPENDED","LICENSE_EXPIRED","DEVICE_RELEASED","DEVICE_CREDENTIAL_REVOKED"}.Contains(a.Code))Invalidate();Publish(false,e);throw;}}finally{mutex.Release();}}
    public Task<Reservation> Reserve(string feature,long quantity,string operationID,CancellationToken ct=default)=>ReservationOperation("reserve",operationID,new(){["feature"]=feature,["quantity"]=quantity},ct);
    public Task<Reservation> Commit(string reservationID,string operationID,CancellationToken ct=default)=>ReservationOperation("commit",operationID,new(){["reservation_id"]=reservationID},ct);
    public Task<Reservation> Cancel(string reservationID,string operationID,CancellationToken ct=default)=>ReservationOperation("cancel",operationID,new(){["reservation_id"]=reservationID},ct);
}
