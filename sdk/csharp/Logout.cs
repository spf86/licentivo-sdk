using System.Text;
using System.Text.Json;

namespace Licentivo;

public sealed record LogoutResult(bool LocalComplete,bool RevocationPending,string StorageError="");

public sealed partial class LicentivoClient {
    private sealed record LogoutTicket(string OperationId,string ProductId,string ActivationId,int ExpectedGeneration,string ReleaseToken);
    private sealed class LogoutJournal {
        public int Version {get;set;}=1;
        public long Revision {get;set;}
        public string SourceHash {get;set;}="";
        public string BindingKey {get;set;}="";
        public bool SignedOut {get;set;}
        public List<LogoutTicket> Pending {get;set;}=[];
    }
    private volatile bool signedOut;
    private LogoutJournal logoutJournal=new();
    private readonly HashSet<string> logoutBindings=[];
    private readonly CancellationTokenSource logoutRecoveryCts=new();
    private Task? logoutRecovery;
    private string LogoutPath=>config.CachePath+".logout.json";
    private string LogoutDirectory=>Path.Combine(Environment.GetEnvironmentVariable("LOCALAPPDATA")??Environment.GetEnvironmentVariable("XDG_CACHE_HOME")??Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile),".cache"),"Licentivo","logout",Hash(Path.GetFullPath(config.CachePath)+"\n"+config.BaseUrl+"\n"+config.ProductId+"\n"+config.DeviceId));
    private static string LogoutBinding(LogoutTicket? t)=>t is null?"":t.ActivationId+":"+t.ExpectedGeneration;
    private static string LogoutScope(LogoutJournal j)=>j.SourceHash+"\n"+j.BindingKey;
    private static bool ExplicitRevocation(Exception error)=>error is ApiException e&&new[]{"LICENSE_REVOKED","LICENSE_SUSPENDED","LICENSE_EXPIRED","DEVICE_RELEASED","DEVICE_NOT_ACTIVE","DEVICE_CREDENTIAL_REVOKED","DEVICE_CREDENTIAL_INVALID","AUTHORIZATION_SESSION_REVOKED","AUTHORIZATION_SESSION_EXPIRED","LAUNCH_LOGIN_REQUIRED","LAUNCH_ENTITLEMENT_REVOKED","LAUNCH_ENTITLEMENT_EXPIRED"}.Contains(e.Code);
    private void LoadLogout(){
        var records=new Dictionary<string,LogoutJournal>();
        void Read(string path){try{
            if(new FileInfo(path).Length>65536)throw new JsonException("invalid logout journal");
            var journal=JsonSerializer.Deserialize<LogoutJournal>(File.ReadAllText(path),JsonOptions)??throw new JsonException("invalid logout journal");
            if(journal.Version!=1||journal.Pending is null)throw new JsonException("invalid logout journal");string scope=LogoutScope(journal);if(!records.TryGetValue(scope,out var old)||journal.Revision>old.Revision||journal.Revision==old.Revision&&(journal.SignedOut||!old.SignedOut))records[scope]=journal;
        }catch(FileNotFoundException){}catch(DirectoryNotFoundException){}catch(Exception e)when(e is IOException or JsonException or UnauthorizedAccessException){signedOut=true;}}
        Read(LogoutPath);
        try{if(Directory.Exists(LogoutDirectory))foreach(string path in Directory.EnumerateFiles(LogoutDirectory,"*.json"))Read(path);}catch(Exception e)when(e is IOException or UnauthorizedAccessException){signedOut=true;}
        string source=CacheHash,binding=LogoutBinding(credential?.LogoutTicket?.Deserialize<LogoutTicket>(JsonOptions));var tickets=new Dictionary<string,LogoutTicket>();
        foreach(var j in records.Values){
            if(j.SignedOut&&j.BindingKey!="")logoutBindings.Add(j.BindingKey);
            if((j.SourceHash==source||config.LaunchConnectionId!=""&&binding!=""&&j.BindingKey==binding||credential is null&&config.IdentityToken==""&&config.LicenseKey=="")&&(j.BindingKey==""||binding==""||j.BindingKey==binding)){logoutJournal=j;if(j.SignedOut&&config.LicenseKey=="")signedOut=true;}
            foreach(var ticket in j.Pending)tickets[ticket.OperationId]=ticket;
        }
        logoutJournal.Pending=tickets.Values.ToList();
        if(signedOut)credential=null;
    }
    private void SaveLogout(){
        logoutJournal.Revision=Math.Max(logoutJournal.Revision+1,DateTimeOffset.UtcNow.ToUnixTimeMilliseconds());
        Exception? fallback=null,primary=null;try{WriteLogout(Path.Combine(LogoutDirectory,Hash(LogoutScope(logoutJournal))+".json"));}catch(Exception e)when(e is IOException or UnauthorizedAccessException){fallback=e;}
        try{WriteLogout(LogoutPath);}catch(Exception e)when(e is IOException or UnauthorizedAccessException){primary=e;}
        if(fallback is not null&&(!logoutJournal.SignedOut||primary is not null))throw new IOException("Logout could not be persisted",fallback);
    }
    private void WriteLogout(string target,LogoutJournal? journal=null){
        string path=Path.GetFullPath(target),dir=Path.GetDirectoryName(path)!;Directory.CreateDirectory(dir);string tmp=Path.Combine(dir,".logout-"+Guid.NewGuid());
        try{using(var file=new FileStream(tmp,FileMode.CreateNew,FileAccess.Write,FileShare.None)){var bytes=Encoding.UTF8.GetBytes(JsonSerializer.Serialize(journal??logoutJournal,JsonOptions));file.Write(bytes);file.Flush(true);}if(!OperatingSystem.IsWindows())File.SetUnixFileMode(tmp,UnixFileMode.UserRead|UnixFileMode.UserWrite);File.Move(tmp,path,true);}finally{if(File.Exists(tmp))File.Delete(tmp);}
    }
    private void AcknowledgeLogout(string operation){
        if(!Directory.Exists(LogoutDirectory))return;foreach(string path in Directory.EnumerateFiles(LogoutDirectory,"*.json")){
            if(new FileInfo(path).Length>65536)throw new JsonException("invalid logout journal");var j=JsonSerializer.Deserialize<LogoutJournal>(File.ReadAllText(path),JsonOptions)??throw new JsonException("invalid logout journal");
            if(j.Pending.RemoveAll(t=>t.OperationId==operation)>0)WriteLogout(path,j);
        }
    }
    private string Deny(){
        signedOut=true;
        if(config.LicenseKey!=""||config.IdentityToken!=""||credential is not null)logoutJournal.SourceHash=CacheHash;
        logoutJournal.SignedOut=true;
        if(credential?.LogoutTicket is { } raw){var ticket=raw.Deserialize<LogoutTicket>(JsonOptions);if(ticket is not null){logoutJournal.BindingKey=LogoutBinding(ticket);logoutBindings.Add(logoutJournal.BindingKey);if(!logoutJournal.Pending.Any(p=>p.OperationId==ticket.OperationId))logoutJournal.Pending.Add(ticket);}}
        string failure="";try{SaveLogout();}catch(IOException){failure="LOCAL_CREDENTIAL_CLEANUP_FAILED";}catch(UnauthorizedAccessException){failure="LOCAL_CREDENTIAL_CLEANUP_FAILED";}
        Invalidate();heartbeatCts?.Cancel();credential=null;credentialDirty=false;config=config with{LicenseKey="",IdentityToken=""};
        StartLogoutRecovery();return failure;
    }
    public async Task<LogoutResult> Logout(CancellationToken ct=default){
        signedOut=true; // Visible to feature checks before waiting for in-flight I/O.
        await mutex.WaitAsync(CancellationToken.None);string storageError;try{storageError=Deny();}finally{mutex.Release();}
        try{await RetryPendingLogouts(ct);}catch(Exception e)when(e is HttpRequestException or IOException or OperationCanceledException or JsonException){}
        bool pending;await mutex.WaitAsync(CancellationToken.None);try{pending=logoutJournal.Pending.Count>0;if(pending)StartLogoutRecovery();}finally{mutex.Release();}
        return new(storageError=="",pending,storageError);
    }
    public async Task RetryPendingLogouts(CancellationToken ct=default){
        await mutex.WaitAsync(ct);try{
            while(logoutJournal.Pending.Count>0){
                var ticket=logoutJournal.Pending[0];using var req=new HttpRequestMessage(HttpMethod.Post,config.BaseUrl+"/api/v1/sessions/logout"){Content=new StringContent(JsonSerializer.Serialize(ticket,JsonOptions),Encoding.UTF8,"application/json")};
                using var deadline=CancellationTokenSource.CreateLinkedTokenSource(ct);deadline.CancelAfter(TimeSpan.FromSeconds(5));var wire=deadline.Token;
                using var res=await http.SendAsync(req,HttpCompletionOption.ResponseHeadersRead,wire);using var stream=await res.Content.ReadAsStreamAsync(wire);using var buffer=new MemoryStream();var chunk=new byte[4096];int count;
                while((count=await stream.ReadAsync(chunk,wire))>0){buffer.Write(chunk,0,count);if(buffer.Length>65536)throw new IOException("oversized logout response");}
                using var doc=Parse(buffer.ToArray(),65536);if((int)res.StatusCode!=200||!doc.RootElement.TryGetProperty("data",out var ack)||!ack.TryGetProperty("status",out var status)||status.GetString() is not ("succeeded" or "stale")||!ack.TryGetProperty("operation_id",out var operation)||operation.GetString()!=ticket.OperationId||!ack.TryGetProperty("activation_id",out var activation)||activation.GetString()!=ticket.ActivationId||!ack.TryGetProperty("expected_generation",out var generation)||!generation.TryGetInt32(out var expected)||expected!=ticket.ExpectedGeneration)throw new IOException("device revocation pending");
                logoutJournal.Pending.RemoveAt(0);AcknowledgeLogout(ticket.OperationId);SaveLogout();
            }
        }finally{mutex.Release();}
    }
    private void StartLogoutRecovery(){
        if(disposed!=0||logoutJournal.Pending.Count==0||logoutRecovery is {IsCompleted:false})return;
        logoutRecovery=Task.Run(async()=>{int delay=1000;while(!logoutRecoveryCts.IsCancellationRequested){try{await Task.Delay(delay,logoutRecoveryCts.Token);using var deadline=CancellationTokenSource.CreateLinkedTokenSource(logoutRecoveryCts.Token);deadline.CancelAfter(TimeSpan.FromSeconds(5));await RetryPendingLogouts(deadline.Token);return;}catch(OperationCanceledException)when(logoutRecoveryCts.IsCancellationRequested){return;}catch{delay=Math.Min(delay*2,300000);}}});
    }
}
