using System.Text.Json;
using System.Text.RegularExpressions;

namespace Licentivo;

public sealed partial class LicentivoClient {
    private sealed record Credential(int Version,string SourceHash,string Origin,string ProductId,string DeviceId,string DeviceKey,string RequestId,string SessionId,JsonElement? LogoutTicket=null);
    private Credential? credential;
    private bool credentialDirty;
    private bool Temporary=>config.LicenseKey!=""||config.IdentityToken!=""||credential is not null;
    private string CacheHash=>config.IdentityToken!=""?Hash(config.IdentityToken):config.LicenseKey==""?credential?.SourceHash??Hash(""):Hash(config.LicenseKey);
    private string RuntimeKey=>credential is {DeviceKey.Length: >0}?credential.DeviceKey:config.LicenseKey;
    private string CredentialPath=>config.CachePath+".credential.json";
    private void LoadCredential(){
        if(string.IsNullOrEmpty(config.CachePath))return;
        try{
            if(new FileInfo(CredentialPath).Length>Limit)return;
            using var doc=Parse(File.ReadAllBytes(CredentialPath));
            var s=doc.RootElement.Deserialize<Credential>(JsonOptions);
            if(s is null||s.Version!=1||s.Origin!=config.BaseUrl||s.SourceHash is null||!Regex.IsMatch(s.SourceHash,"^[0-9a-f]{64}$")||(config.LicenseKey!=""||config.IdentityToken!="")&&s.SourceHash!=CacheHash||s.ProductId!=config.ProductId||s.DeviceId!=config.DeviceId||!Guid.TryParseExact(s.RequestId,"D",out _)||!Guid.TryParseExact(s.SessionId,"D",out _)||s.DeviceKey is null||s.DeviceKey!=""&&!Regex.IsMatch(s.DeviceKey,"^lv_dev_[A-Za-z0-9_-]{43}$")||config.LicenseKey==""&&s.DeviceKey=="")return;
            credential=s;if(s.DeviceKey=="")sessionID=s.SessionId;
        }catch(Exception e)when(e is IOException or JsonException or ArgumentException or InvalidOperationException){}
    }
    private void PrepareCredential(){
        if(!Temporary)return;
        if(string.IsNullOrEmpty(config.CachePath))throw new ArgumentException("cache_path required to retain a transferred license");
        if(credential is null){credential=new(1,CacheHash,config.BaseUrl,config.ProductId,config.DeviceId,"",Guid.NewGuid().ToString(),sessionID);credentialDirty=true;}
        if(credentialDirty)SaveCredential();
    }
    private void AcceptCredential(JsonElement data){
        if(config.LaunchConnectionId!=""&&data.TryGetProperty("logout_ticket",out var candidate)&&logoutBindings.Contains(LogoutBinding(candidate.Deserialize<LogoutTicket>(JsonOptions)))){
            PrepareCredential();credential=credential! with{LogoutTicket=candidate.Clone()};Deny();throw new ApiException(403,"LAUNCH_LOGIN_REQUIRED");
        }
        if(logoutJournal.SignedOut){logoutJournal.SignedOut=false;SaveLogout();}
        if(data.TryGetProperty("logout_ticket",out var ticket)){PrepareCredential();credential=credential! with{LogoutTicket=ticket.Clone()};credentialDirty=true;SaveCredential();}if(!Temporary||credential is {DeviceKey.Length: >0})return;
        string key=OptionalText(data,"device_key");
        if(!Regex.IsMatch(key,"^lv_dev_[A-Za-z0-9_-]{43}$")){if(!config.LicenseKey.StartsWith("lv_tmp_",StringComparison.Ordinal)&&config.IdentityToken=="")return;throw new ArgumentException("server omitted device credential");}
        PrepareCredential();credential=credential! with{DeviceKey=key};credentialDirty=true;SaveCredential();
    }
    private void SaveCredential(){
        string path=Path.GetFullPath(CredentialPath),dir=Path.GetDirectoryName(path)!;Directory.CreateDirectory(dir);string temp=Path.Combine(dir,".credential-"+Guid.NewGuid());
        try{File.WriteAllText(temp,JsonSerializer.Serialize(credential,JsonOptions));if(!OperatingSystem.IsWindows())File.SetUnixFileMode(temp,UnixFileMode.UserRead|UnixFileMode.UserWrite);File.Move(temp,path,true);credentialDirty=false;}finally{if(File.Exists(temp))File.Delete(temp);}
    }
}
