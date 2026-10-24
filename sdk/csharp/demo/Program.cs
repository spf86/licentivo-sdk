using System.Text.Json;using Licentivo;
if(args.Contains("--vectors")){var path=args.Length>1?args[1]:"tests/lease-vectors.json";using var doc=LicentivoClient.Parse(File.ReadAllBytes(path),131072);var root=doc.RootElement;var keys=root.GetProperty("trusted_keys").Deserialize<Dictionary<string,string>>()!;int count=0;foreach(var v in root.GetProperty("vectors").EnumerateArray()){bool ok;try{LicentivoClient.Verify(v.GetProperty("envelope"),keys,v.GetProperty("product_id").GetString()!,v.GetProperty("device_id").GetString()!,v.GetProperty("now").GetInt64());ok=true;}catch{ok=false;}if(ok!=v.GetProperty("valid").GetBoolean())throw new Exception(v.GetProperty("name").GetString());count++;}Console.WriteLine($"C#: {count} lease vectors passed");return;}
var cfgPath = args.Length > 1 && args[0] == "--config" ? args[1] : ".local/protocol-fixture.json";
var cfg = JsonSerializer.Deserialize<Config>(File.ReadAllText(cfgPath), LicentivoClient.JsonOptions)!;
bool cacheTest = args.Contains("--cache-test");
await using (var client = new LicentivoClient(cfg)) {
    if(args.Contains("--logout-wait")){
        if(!client.CheckFeature("export"))throw new Exception("missing initial authorization");File.WriteAllText(cfgPath+".ready","ready");var deadline=DateTime.UtcNow.AddSeconds(30);
        while(!File.Exists(cfgPath+".continue")){if(DateTime.UtcNow>deadline)throw new Exception("logout wait timed out");await Task.Delay(10);}
        var result=await client.Logout();bool expectFailure=args.Contains("--expect-storage-error");if(result.LocalComplete==expectFailure||client.CheckFeature("export"))throw new Exception("unexpected logout persistence result");
        if(expectFailure){bool rejected=false;try{await client.Deactivate();}catch(IOException){rejected=true;}if(!rejected)throw new Exception("deactivate hid persistence failure");}return;
    }
    if(args.Contains("--logout-retry")){await client.RetryPendingLogouts();if(client.CheckFeature("export"))throw new Exception("logout restored access");Console.WriteLine("C#: pending logout completed");return;}
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
