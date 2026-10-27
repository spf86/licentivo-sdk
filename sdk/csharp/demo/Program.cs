using System.Text.Json;using Licentivo;
if(args.Contains("--vectors")){var path=args.Length>1?args[1]:"tests/lease-vectors.json";using var doc=LicentivoClient.Parse(File.ReadAllBytes(path),131072);var root=doc.RootElement;var keys=root.GetProperty("trusted_keys").Deserialize<Dictionary<string,string>>()!;int count=0;foreach(var v in root.GetProperty("vectors").EnumerateArray()){bool ok;try{LicentivoClient.Verify(v.GetProperty("envelope"),keys,v.GetProperty("product_id").GetString()!,v.GetProperty("device_id").GetString()!,v.GetProperty("now").GetInt64());ok=true;}catch{ok=false;}if(ok!=v.GetProperty("valid").GetBoolean())throw new Exception(v.GetProperty("name").GetString());count++;}Console.WriteLine($"C#: {count} lease vectors passed");return;}
var cfgPath = args.Length > 1 && args[0] == "--config" ? args[1] : ".local/protocol-fixture.json";
var cfg = JsonSerializer.Deserialize<Config>(File.ReadAllText(cfgPath), LicentivoClient.JsonOptions)!;
bool cacheTest = args.Contains("--cache-test");
await using (var client = new LicentivoClient(cfg)) {
    if(args.Contains("--offline-lifecycle-test")){
        string phase=File.ReadAllText(cfgPath+".phase");
        void Import(bool fresh=false){string prefix=fresh?".fresh":"";client.ImportOffline(File.ReadAllBytes(cfgPath+prefix+".request.json"),File.ReadAllBytes(cfgPath+prefix+".response.json"));}
        void Reject(Action action){try{action();}catch(Exception){return;}throw new Exception("operation unexpectedly restored access");}
        void Allowed(){if(!client.CheckFeature("export")||client.Status().State!="offline_valid")throw new Exception("offline import did not publish offline access");}
        switch(phase){
            case "export":File.WriteAllText(cfgPath+".request.json",client.OfflineRequest());break;
            case "import":case "retry":Import();if(phase=="retry")Import();Allowed();break;
            case "fail_import":Reject(()=>Import());if(client.CheckFeature("export")||client.Status().Allowed)throw new Exception("failed import granted protected access");break;
            case "release":Import();File.WriteAllText(cfgPath+".release.json",client.OfflineRequest("deactivate"));if(client.CheckFeature("export"))throw new Exception("offline release retained access");break;
            case "signedout":bool failed=false;try{await client.Activate();}catch(Exception){failed=true;}if(!failed)throw new Exception("signed-out credential reactivated");Reject(()=>client.OfflineRequest());Reject(()=>Import());if(client.CheckFeature("export"))throw new Exception("offline release did not survive restart");break;
            case "logout":Import();var result=await client.Logout();if(!result.LocalComplete||result.RevocationPending||client.CheckFeature("export"))throw new Exception("logout did not complete");break;
            case "old_export":case "renew_request":
                if(phase=="old_export"){Reject(()=>Import());if(client.CheckFeature("export"))throw new Exception("old approval restored logged-out access");}
                File.WriteAllText(cfgPath+".fresh.request.json",client.OfflineRequest());
                if(phase=="renew_request"){Reject(()=>Import());if(client.CheckFeature("export"))throw new Exception("old approval replaced a newer request");}break;
            case "fresh_import":Import(true);Allowed();Reject(()=>Import());Allowed();Import(true);Allowed();break;
            default:throw new Exception("unknown offline lifecycle phase");
        }
        Console.WriteLine("C#: offline lifecycle phase passed "+phase);return;
    }
    if(args.Contains("--logout-wait")){
        if(!client.CheckFeature("export"))throw new Exception("missing initial authorization");File.WriteAllText(cfgPath+".ready","ready");var deadline=DateTime.UtcNow.AddSeconds(30);
        while(!File.Exists(cfgPath+".continue")){if(DateTime.UtcNow>deadline)throw new Exception("logout wait timed out");await Task.Delay(10);}
        var result=await client.Logout();bool expectFailure=args.Contains("--expect-storage-error");if(result.LocalComplete==expectFailure||client.CheckFeature("export"))throw new Exception("unexpected logout persistence result");
        if(expectFailure){bool rejected=false;try{await client.Deactivate();}catch(IOException){rejected=true;}if(!rejected)throw new Exception("deactivate hid persistence failure");}return;
    }
    if(args.Contains("--logout-retry")){await client.RetryPendingLogouts();if(client.CheckFeature("export"))throw new Exception("logout restored access");Console.WriteLine("C#: pending logout completed");return;}
    if(args.Contains("--metering-boundary-test")) {
        await client.Activate();int calls=0;bool denied=args.Contains("--expect-denied"),recover=args.Contains("--metering-recover-test");Func<Task> action=()=>{calls++;return Task.CompletedTask;};
        if(recover)for(int i=0;i<2;i++)await client.ResolveMeteredFeature("boundary-job",!denied);
        bool failed=false;try{await client.RunMeteredFeature("export",1,"boundary-job",action);}catch(Exception){failed=true;}
        if(failed!=denied||calls!=(recover||denied?0:1))throw new Exception("SDK executed outside authorization or repeated business");
        if(recover&&denied){await client.RunMeteredFeature("export",1,"replacement-job",action);if(calls!=1)throw new Exception("cancellation did not release quota");}
        Console.WriteLine("C#: execution boundary regression passed");return;
    }
    if(args.Contains("--metering-expired-cancel-test")||args.Contains("--metering-expired-commit-test")) {
        bool cancel=args.Contains("--metering-expired-cancel-test"),recover=args.Contains("--metering-recover-test");int calls=0;
        async Task ExpectFailure(Func<Task> action){try{await action();}catch(Exception){return;}throw new Exception("expected operation failure");}
        Task Action(){calls++;return Task.CompletedTask;}
        if(!recover){
            await client.Activate();
            await ExpectFailure(()=>client.RunMeteredFeature("export",1,"expired-job",()=>{calls++;if(cancel)throw new Exception("expected business failure");return Task.CompletedTask;}));
            if(calls!=1)throw new Exception("business outcome was lost");
        }else if(cancel){
            for(int i=0;i<2;i++)await client.ResolveMeteredFeature("expired-job",false);
            await ExpectFailure(()=>client.ResolveMeteredFeature("expired-job",true));
            await ExpectFailure(()=>client.RunMeteredFeature("export",1,"expired-job",Action));
            if(calls!=0)throw new Exception("canceled business repeated");
            await client.RunMeteredFeature("export",1,"replacement-job",Action);
            if(calls!=1)throw new Exception("expired cancellation did not release quota");
        }else{
            await ExpectFailure(()=>client.RunMeteredFeature("export",1,"expired-job",Action));
            await ExpectFailure(()=>client.ResolveMeteredFeature("expired-job",true));
            await ExpectFailure(()=>client.ResolveMeteredFeature("expired-job",false));
            if(calls!=0)throw new Exception("completed business repeated");
        }
        Console.WriteLine("C#: expired task regression passed");return;
    }
    if(args.Any(arg=>arg.StartsWith("--metering-",StringComparison.Ordinal))) {
        await client.Activate();int calls=0;Func<Task> action=()=>{calls++;return Task.CompletedTask;};
        if(args.Contains("--metering-jobs-test")) {
            foreach(var job in new[]{"ExportJob","exportjob","ExportJob"})await client.RunMeteredFeature("export",1,job,action);
            if(calls!=2)throw new Exception("distinct jobs were skipped or repeated");
        } else if(args.Contains("--metering-features-test")) {
            foreach(var feature in new[]{"export","Export"})if((await client.Consume(feature,1,feature+"-job")).GetProperty("used").GetInt64()!=1)throw new Exception("feature meters were combined");
        } else {
            await client.Reserve("export",1,"pending-job");bool failed=false;
            try{await client.RunMeteredFeature("export",1,"pending-job",action);}catch(Exception){failed=true;}
            if(!failed||calls!=0)throw new Exception("pending job bypassed current authorization");
        }
        Console.WriteLine("C#: metering regression passed");return;
    }
    if(args.Contains("--improvements-test")) {
        await client.Start();var one=await client.Reserve("export",1,"cancel-job");if(one.Status!="pending"||one.Consumption.Used!=0||one.Consumption.Reserved!=1)throw new Exception("reserve failed");var canceled=await client.Cancel(one.ReservationId,"cancel-job");if(canceled.Status!="canceled"||canceled.Consumption.Used!=0)throw new Exception("cancel charged usage");
        var two=await client.Reserve("export",1,"commit-job");for(int i=0;i<2;i++){var done=await client.Commit(two.ReservationId,"commit-job");if(done.Status!="committed"||done.Consumption.Used!=1)throw new Exception("duplicate commit");}
        if((await client.Reserve("export",1,"commit-job")).Status!="committed")throw new Exception("unsafe business replay");bool failed=false;try{await client.Consume("export",1000,"quota-job");}catch(ApiException e){failed=e.Code=="FEATURE_QUOTA_EXCEEDED"&&e.RequestID!=""&&!e.Retryable;}
        if(!failed||client.Status().State!="quota_exhausted"||!client.CheckFeature("export"))throw new Exception("typed quota status missing");await client.DisposeAsync();await using var restored=new LicentivoClient(cfg with{LicenseKey=""});await restored.Start();await restored.Validate();if(!restored.CheckFeature("export"))throw new Exception("normal credential not retained");await restored.Deactivate();if(restored.Status().Allowed)throw new Exception("release grants access");Console.WriteLine("C#: credential / reservation / typed status OK");return;
    }
    if(args.Contains("--startup-test")){var timer=System.Diagnostics.Stopwatch.StartNew();await client.Start();if(timer.ElapsedMilliseconds>300)throw new Exception("cached startup blocked");await Task.Delay(100);timer.Restart();if(!client.CheckFeature("export")||timer.ElapsedMilliseconds>300)throw new Exception("local feature check blocked");Console.WriteLine("C#: cached startup and local feature checks stay responsive");return;}
    if(args.Contains("--offline-export")) {await File.WriteAllTextAsync(cfgPath+".request.json",client.OfflineRequest());Console.WriteLine("C#: offline request exported");return;}
    if(args.Contains("--transfer-test")||args.Contains("--transfer-import")) {
        if(args.Contains("--transfer-import"))client.ImportOffline(await File.ReadAllBytesAsync(cfgPath+".request.json"),await File.ReadAllBytesAsync(cfgPath+".response.json"));else await client.Start();
        await client.Validate();await client.Heartbeat();if(!client.CheckFeature("export"))throw new Exception("feature denied");Console.WriteLine("C#: transferred device credential OK");return;
    }
    if(args.Contains("--floating-test")) {
        await client.Start();await using var other=new LicentivoClient(cfg);
        if(other.CheckFeature("export"))throw new Exception("session cache reused");await other.Start();
        await client.DisposeAsync();await other.Heartbeat();if(!other.CheckFeature("export"))throw new Exception("new session denied");Console.WriteLine("C#: floating seat release OK");return;
    }
    if(args.Contains("--advanced-test")||args.Contains("--offline-import")) {
        if(args.Contains("--offline-import"))client.ImportOffline(await File.ReadAllBytesAsync(cfgPath+".request.json"),await File.ReadAllBytesAsync(cfgPath+".response.json"));else await client.Start();
        if(!client.CheckFeature("export"))throw new Exception("feature denied");
        if((await client.Consume("export",1,"business-job-1")).GetProperty("used").GetInt64()!=1||(await client.Consume("export",1,"business-job-1")).GetProperty("used").GetInt64()!=1)throw new Exception("consume replay failed");
        var over=await client.Consume("export",3,"business-job-2");if(over.GetProperty("used").GetInt64()!=4||over.GetProperty("overage").GetInt64()!=1)throw new Exception("overage failed");
        bool failed=false;try{await client.Consume("export",1,"business-job-3");}catch(ApiException e){failed=e.Code=="FEATURE_QUOTA_EXCEEDED";}
        if(!failed||!client.CheckFeature("export"))throw new Exception("quota denial failed");await client.Deactivate();Console.WriteLine("C#: offline/metered feature checks OK");return;
    }
    if(args.Contains("--refresh-test")) {
        bool failed=false;try{await client.RefreshOnline();}catch{failed=true;}
        if(!failed||!client.CheckFeature("export"))throw new Exception("refresh failure did not retain offline access");
        await client.RefreshOnline();if(!client.CheckFeature("updated")||client.CheckFeature("export"))throw new Exception("online refresh did not replace cached policy");
        failed=false;try{await client.RefreshOnline();}catch{failed=true;}
        if(!failed||client.CheckFeature("updated"))throw new Exception("refresh denial did not clear cache");
        Console.WriteLine("C#: online refresh OK");return;
    }
    if(args.Contains("--policy-test")) {
        bool deny=args.Contains("--expect-denied");if(client.CheckFeature("export")==deny)throw new Exception("unexpected cache device/policy access");
        if(!deny){await client.Activate();await client.Validate();await client.Heartbeat();}Console.WriteLine("C#: policy cache OK");return;
    }
    var claims = await client.Activate();
    Console.WriteLine("C#: activated " + claims.ActivationId);
    if (!cacheTest) {
        await client.Validate(); await client.Heartbeat();
        Console.WriteLine("C#: export enabled " + client.CheckFeature("export"));
        await client.Deactivate();
        Console.WriteLine("C#: deactivated");
        return;
    }
}
await using (var restored = new LicentivoClient(cfg)) {
    await restored.Validate();
    if (!restored.CheckFeature("export")) throw new Exception("cache not restored during server outage");
    bool rejected = false;
    try { await restored.Validate(); } catch (ApiException e) { rejected = e.Status == 403; }
    if (!rejected || restored.CheckFeature("export")) throw new Exception("rejection did not invalidate cache");
}
await using (var reopened = new LicentivoClient(cfg)) {
    if (reopened.CheckFeature("export")) throw new Exception("rejected cache survived restart");
}
Console.WriteLine("C#: cache restored on 503; 403 invalidated access and persisted cache");
