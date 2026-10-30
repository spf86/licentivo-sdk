using System.Diagnostics;
using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using Org.BouncyCastle.Crypto.Parameters;
using Org.BouncyCastle.Crypto.Signers;

namespace Licentivo;

public sealed record Config(string BaseUrl, string ProductId, string LicenseKey, string DeviceId, string DeviceName, Dictionary<string,string> TrustedKeys, string CachePath, bool AllowHttp, string AppVersion="",int TimeoutSeconds=10,string ProxyUrl="",string LaunchConnectionId="",string IdentityToken="",string Environment="");
public sealed record Claims(string ActivationId, string ProductId, string DeviceId, string[] Features, long IssuedAt, long ExpiresAt, string OfflineMode="limited", int HeartbeatSeconds=3600, string LicenseModel="node_locked", string AppVersion="", string SessionId="", string OfflineRequestId="",string LaunchConnectionId="",string Environment="",long BindingVersion=0);
public sealed class ApiException(int status,string code,string requestID="",int retryAfterSeconds=0):Exception($"Licentivo {code} ({status})") {public int Status {get;}=status;public string Code {get;}=code;public string RequestID {get;}=requestID;public bool Retryable {get;}=status==429||status>=500;public int RetryAfterSeconds {get;}=retryAfterSeconds;}

public sealed partial class LicentivoClient : IAsyncDisposable {
    private const int Limit=16384;
    public static readonly JsonSerializerOptions JsonOptions=new(){PropertyNamingPolicy=JsonNamingPolicy.SnakeCaseLower,PropertyNameCaseInsensitive=false};
    private readonly DeviceProfile profile;private string sessionID=Guid.NewGuid().ToString();private Config config; private readonly HttpClient http;private readonly SemaphoreSlim mutex=new(1,1);
    private JsonElement? cached;private long wall;private long tick=Stopwatch.GetTimestamp();private volatile int interval=3600;private CancellationTokenSource? heartbeatCts;private Task? heartbeatTask;
    public LicentivoClient(Config cfg){
        profile=MachineIdentity.Profile(cfg.ProductId);if(string.IsNullOrWhiteSpace(cfg.DeviceName))cfg=cfg with{DeviceName=DefaultDeviceName()};cfg=cfg with{LicenseKey=cfg.LicenseKey??"",BaseUrl=(string.IsNullOrEmpty(cfg.BaseUrl)?"https://www.licentivo.com":cfg.BaseUrl).TrimEnd('/')};if(string.IsNullOrEmpty(cfg.CachePath))cfg=cfg with{CachePath=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Licentivo",Hash(cfg.BaseUrl+"\n"+cfg.ProductId),"lease.json")};config=cfg with{DeviceId=MachineIdentity.DeviceId(cfg.ProductId,profile)};cfg=config;var u=new Uri(cfg.BaseUrl);if(u.UserInfo!=""||u.Query!=""||u.Fragment!=""||u.AbsolutePath!="/")throw new ArgumentException("base_url must be an origin");
        if(u.Scheme!="https"&&!(cfg.AllowHttp&&u.Scheme=="http"&&u.IsLoopback))throw new ArgumentException("HTTPS required");
        if(!System.Text.RegularExpressions.Regex.IsMatch(cfg.DeviceId,"^[a-f0-9]{64}$")||cfg.LicenseKey!=""&&!System.Text.RegularExpressions.Regex.IsMatch(cfg.LicenseKey,"^lv_(lic|tmp)_[A-Za-z0-9_-]{43}$")||cfg.TrustedKeys.Count==0)throw new ArgumentException("invalid licensing configuration");foreach(var pub in cfg.TrustedKeys.Values)if(Decode(pub).Length!=32)throw new ArgumentException("invalid pinned key");
        LoadCredential();LoadLogout();if(cfg.LicenseKey==""&&cfg.IdentityToken==""&&credential is null&&!signedOut)throw new ArgumentException("enter a license key or temporary activation code");if(cfg.TimeoutSeconds<1||cfg.TimeoutSeconds>120)throw new ArgumentException("timeout_seconds must be 1–120");var handler=new HttpClientHandler{AllowAutoRedirect=false};if(cfg.ProxyUrl!=""){handler.Proxy=new WebProxy(cfg.ProxyUrl);handler.UseProxy=true;}http=new HttpClient(handler){Timeout=TimeSpan.FromSeconds(cfg.TimeoutSeconds)};
        try{if(!signedOut&&!logoutJournal.SignedOut&&cfg.CachePath!=""&&File.Exists(cfg.CachePath)&&new FileInfo(cfg.CachePath).Length<=Limit){using var doc=Parse(File.ReadAllBytes(cfg.CachePath));var s=doc.RootElement;if(s.GetProperty("version").GetInt32()==1&&s.GetProperty("license_hash").GetString()==CacheHash&&Now+300>=s.GetProperty("observed_at").GetInt64()){var claims=Verify(s.GetProperty("lease"),cfg.TrustedKeys,cfg.ProductId,cfg.DeviceId,Now);if(claims.OfflineMode!="none"&&claims.LicenseModel!="floating"&&claims.AppVersion==cfg.AppVersion){cached=s.GetProperty("lease").Clone();wall=s.GetProperty("observed_at").GetInt64();interval=claims.HeartbeatSeconds;}}}}catch(Exception e)when(e is IOException or JsonException or ArgumentException or CryptographicException or KeyNotFoundException or InvalidOperationException){}
        Publish(false,null);StartLogoutRecovery();
    }
    private static long Now=>DateTimeOffset.UtcNow.ToUnixTimeSeconds();
    private static string Hash(string s)=>Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(s))).ToLowerInvariant();
    public static string DeviceId(string product,string stableInstallationId)=>Hash("licentivo-device-v1\n"+product+"\n"+stableInstallationId.Trim().ToLowerInvariant());
    public static byte[] Decode(string s){if(!System.Text.RegularExpressions.Regex.IsMatch(s,"^[A-Za-z0-9_-]+$"))throw new ArgumentException("invalid base64url");var b=Convert.FromBase64String(s.Replace('-','+').Replace('_','/')+new string('=',(4-s.Length%4)%4));if(Convert.ToBase64String(b).TrimEnd('=').Replace('+','-').Replace('/','_')!=s)throw new ArgumentException("noncanonical base64url");return b;}
    public static JsonDocument Parse(byte[] bytes,int max=Limit){if(bytes.Length>max)throw new JsonException("JSON size limit");var doc=JsonDocument.Parse(bytes,new JsonDocumentOptions{MaxDepth=32});try{Unique(doc.RootElement);return doc;}catch{doc.Dispose();throw;}}
    private static void Unique(JsonElement e){if(e.ValueKind==JsonValueKind.Object){var names=new HashSet<string>();foreach(var p in e.EnumerateObject()){if(!names.Add(p.Name))throw new JsonException("duplicate field");Unique(p.Value);}}else if(e.ValueKind==JsonValueKind.Array)foreach(var v in e.EnumerateArray())Unique(v);}
    private static void Fields(JsonElement e,params string[] fields){if(e.ValueKind!=JsonValueKind.Object||!e.EnumerateObject().Select(p=>p.Name).ToHashSet().SetEquals(fields))throw new JsonException("invalid fields");}
    public static Claims Verify(JsonElement envelope,Dictionary<string,string> keys,string product,string device,long now){
        Fields(envelope,"version","key_id","payload","signature");if(envelope.GetProperty("version").GetInt32()!=1)throw new CryptographicException("invalid version");var keyId=envelope.GetProperty("key_id").GetString()!;if(!keys.TryGetValue(keyId,out var pinned))throw new CryptographicException("unknown pinned key");var raw=Decode(envelope.GetProperty("payload").GetString()!);var sig=Decode(envelope.GetProperty("signature").GetString()!);var pub=Decode(pinned);if(raw.Length>Limit||sig.Length!=64||pub.Length!=32)throw new CryptographicException("invalid lengths");
        var signer=new Ed25519Signer();signer.Init(false,new Ed25519PublicKeyParameters(pub,0));byte[] prefix=Encoding.UTF8.GetBytes("LicenovaLease/v1\n");signer.BlockUpdate(prefix,0,prefix.Length);signer.BlockUpdate(raw,0,raw.Length);if(!signer.VerifySignature(sig))throw new CryptographicException("bad lease signature");
        using var doc=Parse(raw);var c=doc.RootElement;var names=new List<string>{"version","key_id","tenant_id","product_id","license_id","device_id","activation_id","mode","features","max_devices","issued_at","expires_at"};if(c.TryGetProperty("runtime_policy",out _))names.Add("runtime_policy");foreach(var field in new[]{"capabilities","offline_request_id"})if(c.TryGetProperty(field,out _))names.Add(field);Fields(c,names.ToArray());if(c.GetProperty("version").GetInt32()!=1||c.GetProperty("key_id").GetString()!=keyId||c.GetProperty("product_id").GetString()!=product||c.GetProperty("device_id").GetString()!=device||!System.Text.RegularExpressions.Regex.IsMatch(device,"^[0-9a-f]{64}$"))throw new CryptographicException("context mismatch");
        foreach(var f in new[]{"tenant_id","product_id","license_id","activation_id"})if(!Guid.TryParseExact(c.GetProperty(f).GetString(),"D",out _))throw new CryptographicException("invalid identifier");var mode=c.GetProperty("mode").GetString();var max=c.GetProperty("max_devices").GetInt64();long issued=c.GetProperty("issued_at").GetInt64(),expires=c.GetProperty("expires_at").GetInt64();var policy=Policy(c,issued);if(!new[]{"perpetual","timed","trial"}.Contains(mode)||max<1||max>10000||issued<=0||expires<=issued||expires-issued>policy.Span||expires>253402300799L||issued>now+300||now>=expires)throw new CryptographicException("expired lease/clock");
        var features=c.GetProperty("features").EnumerateArray().Select(v=>v.GetString()!).ToArray();if(features.Length>32||features.Any(f=>f is null||!System.Text.RegularExpressions.Regex.IsMatch(f,"^[A-Za-z0-9_.-]{1,64}$"))||features.Zip(features.Skip(1)).Any(pair=>string.CompareOrdinal(pair.First,pair.Second)>=0))throw new CryptographicException("invalid features");VerifyCapabilities(c);var capabilities=c.TryGetProperty("capabilities",out var caps)?caps:default;return new Claims(c.GetProperty("activation_id").GetString()!,product,device,features,issued,expires,policy.Mode,policy.Heartbeat,capabilities.ValueKind==JsonValueKind.Object?capabilities.GetProperty("license_model").GetString()!:"node_locked",OptionalText(capabilities,"app_version"),OptionalText(capabilities,"session_id"),OptionalText(c,"offline_request_id"),OptionalText(capabilities,"launch_connection_id"),OptionalText(capabilities,"environment"),OptionalNumber(capabilities,"binding_version"));
    }
    private static (string Mode,int Heartbeat,long Span) Policy(JsonElement c,long issued){
        if(!c.TryGetProperty("runtime_policy",out var p))return ("limited",3600,86400);
        Fields(p,"offline_mode","heartbeat_seconds","offline_seconds");string mode=p.GetProperty("offline_mode").GetString()!;int h=p.GetProperty("heartbeat_seconds").GetInt32(),o=p.GetProperty("offline_seconds").GetInt32();
        if(h!=0&&(h<10||h>86400))throw new CryptographicException("invalid heartbeat interval");
        if(mode=="permanent"&&o==0)return (mode,h,253402300799L-issued);
        if(mode=="none"&&o==0)return (mode,h,h==0?10:h+(h+9)/10+30);
        if(mode=="limited"&&o>=1&&o<=31536000)return (mode,h,o);
        throw new CryptographicException("invalid runtime policy");
    }
    private Claims Check(){if(signedOut)throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");if(wall>0&&Now<wall+Stopwatch.GetElapsedTime(tick).TotalSeconds-300)throw new CryptographicException("clock rollback; verify online");if(cached is null)throw new CryptographicException("no valid lease");var claims=Verify(cached.Value,config.TrustedKeys,config.ProductId,config.DeviceId,Now);CheckContext(claims);return claims;}
    public bool CheckFeature(string name)=>FeatureStatus(name).Allowed;
    private void Invalidate(){cached=null;Publish(false,null);if(config.CachePath!=""&&!signedOut)try{File.Delete(config.CachePath);}catch(IOException){}}
    private void Save(){if(config.CachePath=="")return;string path=Path.GetFullPath(config.CachePath),dir=Path.GetDirectoryName(path)!;Directory.CreateDirectory(dir);string temp=Path.Combine(dir,".lease-"+Guid.NewGuid());try{File.WriteAllText(temp,JsonSerializer.Serialize(new{version=1,license_hash=CacheHash,lease=cached,observed_at=wall}));if(!OperatingSystem.IsWindows())File.SetUnixFileMode(temp,UnixFileMode.UserRead|UnixFileMode.UserWrite);File.Move(temp,path,true);snapshot=new(cached,wall,tick);}finally{if(File.Exists(temp))File.Delete(temp);}}
    private async Task<JsonElement> Request(string op,string activation,CancellationToken ct,string? suppliedID=null,Dictionary<string,object>? extra=null){
        if(signedOut)throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");PrepareCredential();string wireOp=op=="activate"&&credential?.DeviceKey is not {Length:>0}&&config.IdentityToken!=""?"launch/exchange":op;var input=AccountBody(activation);if(extra is not null)foreach(var pair in extra)input[pair.Key]=pair.Value;string body=JsonSerializer.Serialize(input,JsonOptions);string idem=op=="activate"&&Temporary&&credential!.DeviceKey==""&&cached is null?credential.RequestId:suppliedID??Guid.NewGuid().ToString();Exception? last=null;
        for (int attempt=0; attempt<2; attempt++) {
            if (attempt>0) await Task.Delay(200,ct);
            using var deadline=CancellationTokenSource.CreateLinkedTokenSource(ct);
            deadline.CancelAfter(TimeSpan.FromSeconds(config.TimeoutSeconds));
            using var req=new HttpRequestMessage(HttpMethod.Post,config.BaseUrl.TrimEnd('/')+"/api/v1/"+wireOp){Content=new StringContent(body,Encoding.UTF8,"application/json")};
            req.Headers.Add("Idempotency-Key",idem);
            try {
                using var res=await http.SendAsync(req,HttpCompletionOption.ResponseHeadersRead,deadline.Token);
                
                await using var stream=await res.Content.ReadAsStreamAsync(deadline.Token);
                using var memory=new MemoryStream();var buffer=new byte[4096];int n;
                while ((n=await stream.ReadAsync(buffer,deadline.Token))>0) {
                    if (memory.Length+n>Limit) throw new JsonException("oversized response");
                    memory.Write(buffer,0,n);
                }
                JsonDocument doc;
                try{doc=Parse(memory.ToArray());}
                catch(JsonException)when((int)res.StatusCode==429||(int)res.StatusCode>=500){int retry=(int)Math.Clamp(res.Headers.RetryAfter?.Delta?.TotalSeconds??0,0,86400);var error=new ApiException((int)res.StatusCode,"UNEXPECTED_RESPONSE",res.Headers.TryGetValues("X-Request-ID",out var ids)?ids.FirstOrDefault()??"":"",retry);if((int)res.StatusCode>=500){last=error;continue;}throw error;}
                using var parsed=doc;var env=doc.RootElement;
                if (res.StatusCode!=HttpStatusCode.OK||env.TryGetProperty("error",out _)) {
                    string code=env.TryGetProperty("error",out var error)?error.GetProperty("code").GetString()!:"UNEXPECTED_RESPONSE";
                    int retry=(int)Math.Clamp(res.Headers.RetryAfter?.Delta?.TotalSeconds??(res.Headers.RetryAfter?.Date-DateTimeOffset.UtcNow)?.TotalSeconds??0,0,86400);var apiError=new ApiException((int)res.StatusCode,code,OptionalText(env,"request_id"),retry);if((int)res.StatusCode>=500){last=apiError;continue;}if(ExplicitRevocation(apiError))Deny();else if(AuthorizationRejected(apiError))Invalidate();throw apiError;
                }
                return env.GetProperty("data").Clone();
            } catch (Exception e) when (e is HttpRequestException or OperationCanceledException or IOException) {
                if (ct.IsCancellationRequested) throw;
                last=e;
            }
        }
        throw last??new HttpRequestException("no response");
    }
    private async Task<Claims> Call(string op,CancellationToken ct,bool forceOnline=false){await mutex.WaitAsync(ct);try{if(signedOut)throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");try{var cachedClaims=Check();if(!forceOnline&&op=="activate"&&cachedClaims.OfflineMode=="permanent"&&cachedClaims.HeartbeatSeconds==0)return cachedClaims;}catch{}string activation="";if(op!="activate"){if(cached is null)throw new InvalidOperationException("activate first");using var d=Parse(Decode(cached.Value.GetProperty("payload").GetString()!));activation=d.RootElement.GetProperty("activation_id").GetString()!;}JsonElement data;try{data=await Request(op,activation,ct);}catch(Exception e){bool temporary=e is HttpRequestException or OperationCanceledException or IOException||e is ApiException a&&a.Retryable;if(temporary&&!ct.IsCancellationRequested)try{var cachedClaims=Check();if(!forceOnline&&cachedClaims.OfflineMode!="none"){Publish(false,e);return cachedClaims;}if(cachedClaims.OfflineMode=="none")Invalidate();}catch{}if(!temporary)Invalidate();Publish(false,e);throw;}if(signedOut){if(data.TryGetProperty("logout_ticket",out var pendingTicket)&&credential is not null)credential=credential with{LogoutTicket=pendingTicket.Clone()};throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");}try{var envelope=data.GetProperty("lease");var claims=Verify(envelope,config.TrustedKeys,config.ProductId,config.DeviceId,Now);CheckContext(claims);if(claims.ActivationId!=data.GetProperty("activation_id").GetString())throw new CryptographicException("activation mismatch");AcceptCredential(data);cached=envelope.Clone();wall=Now;tick=Stopwatch.GetTimestamp();interval=claims.HeartbeatSeconds;using(var payload=Parse(Decode(envelope.GetProperty("payload").GetString()!))){if(!payload.RootElement.TryGetProperty("runtime_policy",out _)&&data.TryGetProperty("heartbeat_seconds",out var hb))interval=Math.Clamp(hb.GetInt32(),10,86400);}Save();Publish(true,null);return claims;}catch{Invalidate();throw;}}finally{mutex.Release();}}
    public Task<Claims> Activate(CancellationToken ct=default)=>Call("activate",ct);public Task<Claims> Validate(CancellationToken ct=default)=>Call("validate",ct);public Task<Claims> Heartbeat(CancellationToken ct=default)=>Call("heartbeat",ct);
    public Task<Claims> RefreshOnline(CancellationToken ct=default)=>Call("activate",ct,true);
    public async Task Deactivate(CancellationToken ct=default){var result=await Logout(ct);if(!result.LocalComplete)throw new IOException(result.StorageError);}
    private double HeartbeatDelay(){double delay=Math.Max(600,interval)*(1+Random.Shared.NextDouble()*.1);try{double remaining=LocalCheck().ExpiresAt-DateTimeOffset.UtcNow.ToUnixTimeMilliseconds()/1000.0;if(remaining>0)delay=Math.Min(delay,Math.Max(1,remaining*.8));}catch{}return Math.Max(delay,(retryAt-DateTimeOffset.UtcNow).TotalSeconds);}
    public void StartHeartbeat(Action<Exception>? onError=null){try{if(interval==0)return;}catch{}if(heartbeatTask is not null&&!heartbeatTask.IsCompleted)return;heartbeatCts?.Dispose();heartbeatCts=new();var ct=heartbeatCts.Token;heartbeatTask=Task.Run(async()=>{while(!ct.IsCancellationRequested){if(interval==0)return;try{await Task.Delay(TimeSpan.FromSeconds(HeartbeatDelay()),ct);await Heartbeat(ct);}catch(OperationCanceledException)when(ct.IsCancellationRequested){return;}catch(Exception e){if(onError is not null)_=Task.Run(()=>onError(e));}}},ct);}
    private int disposed;
    public async ValueTask DisposeAsync(){if(Interlocked.Exchange(ref disposed,1)!=0)return;await logoutRecoveryCts.CancelAsync();if(logoutRecovery is not null)try{await logoutRecovery;}catch(OperationCanceledException){}await startupCts.CancelAsync();if(heartbeatCts is not null)await heartbeatCts.CancelAsync();foreach(var task in new[]{heartbeatTask,refreshTask})if(task is not null)try{await task;}catch(OperationCanceledException){}heartbeatCts?.Dispose();startupCts.Dispose();try{using var deadline=new CancellationTokenSource(TimeSpan.FromSeconds(5));if(Check().LicenseModel=="floating"){var id=Check().ActivationId;Invalidate();await Request("deactivate",id,deadline.Token);}}catch{}http.Dispose();mutex.Dispose();}
}
