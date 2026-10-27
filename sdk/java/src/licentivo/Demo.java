package licentivo;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Demo {
    private static void expectFailure(Client.FeatureAction action) throws Exception {
        try { action.run(); } catch (Exception error) { return; }
        throw new Exception("expected operation failure");
    }
    private static void offlineLifecycle(Client client,String path)throws Exception{
        String phase=Files.readString(Path.of(path+".phase"));
        Client.FeatureAction old=()->client.importOffline(Files.readString(Path.of(path+".request.json")),Files.readString(Path.of(path+".response.json")));
        Client.FeatureAction fresh=()->client.importOffline(Files.readString(Path.of(path+".fresh.request.json")),Files.readString(Path.of(path+".fresh.response.json")));
        Client.FeatureAction allowed=()->{if(!client.checkFeature("export")||!"offline_valid".equals(client.status().get("state")))throw new Exception("offline import did not publish offline access");};
        switch(phase){
            case "export"->Files.writeString(Path.of(path+".request.json"),client.offlineRequest("activate"));
            case "import","retry"->{old.run();if(phase.equals("retry"))old.run();allowed.run();}
            case "fail_import"->{expectFailure(old);if(client.checkFeature("export")||Boolean.TRUE.equals(client.status().get("allowed")))throw new Exception("failed import granted protected access");}
            case "release"->{old.run();Files.writeString(Path.of(path+".release.json"),client.offlineRequest("deactivate"));if(client.checkFeature("export"))throw new Exception("offline release retained access");}
            case "signedout"->{expectFailure(()->client.activate());expectFailure(()->client.offlineRequest("activate"));expectFailure(old);if(client.checkFeature("export"))throw new Exception("offline release did not survive restart");}
            case "logout"->{old.run();var result=client.logout();if(!Boolean.TRUE.equals(result.get("local_complete"))||Boolean.TRUE.equals(result.get("revocation_pending"))||client.checkFeature("export"))throw new Exception("logout did not complete");}
            case "old_export","renew_request"->{
                if(phase.equals("old_export")){expectFailure(old);if(client.checkFeature("export"))throw new Exception("old approval restored logged-out access");}
                Files.writeString(Path.of(path+".fresh.request.json"),client.offlineRequest("activate"));
                if(phase.equals("renew_request")){expectFailure(old);if(client.checkFeature("export"))throw new Exception("old approval replaced a newer request");}
            }
            case "fresh_import"->{fresh.run();allowed.run();expectFailure(old);allowed.run();fresh.run();allowed.run();}
            default->throw new Exception("unknown offline lifecycle phase");
        }
        System.out.println("Java: offline lifecycle phase passed "+phase);
    }
    public static void main(String[] args) throws Exception {
        String path = args.length > 1 && args[0].equals("--config") ? args[1] : ".local/protocol-fixture.json";
        var cfg = Json.object(Json.parse(Files.readString(Path.of(path))));
        boolean cacheTest = Arrays.asList(args).contains("--cache-test");
        try (Client client = new Client(cfg)) {
            if(Arrays.asList(args).contains("--offline-lifecycle-test")){offlineLifecycle(client,path);return;}
            if(Arrays.asList(args).contains("--logout-wait")){
                if(!client.checkFeature("export"))throw new Exception("missing initial authorization");Files.writeString(Path.of(path+".ready"),"ready");long deadline=System.nanoTime()+30000000000L;
                while(!Files.exists(Path.of(path+".continue"))){if(System.nanoTime()>deadline)throw new Exception("logout wait timed out");Thread.sleep(10);}
                var result=client.logout();boolean expectFailure=Arrays.asList(args).contains("--expect-storage-error");
                if(Boolean.TRUE.equals(result.get("local_complete"))==expectFailure||client.checkFeature("export"))throw new Exception("unexpected logout persistence result");
                if(expectFailure){boolean rejected=false;try{client.deactivate();}catch(java.io.IOException error){rejected=true;}if(!rejected)throw new Exception("deactivate hid persistence failure");}return;
            }
            if(Arrays.asList(args).contains("--logout-retry")){client.retryPendingLogouts();if(client.checkFeature("export"))throw new Exception("logout restored access");System.out.println("Java: pending logout completed");return;}
            if(Arrays.asList(args).contains("--metering-boundary-test")) {
                client.activate();int[] calls={0};boolean denied=Arrays.asList(args).contains("--expect-denied"),recover=Arrays.asList(args).contains("--metering-recover-test");
                if(recover)for(int i=0;i<2;i++)client.resolveMeteredFeature("boundary-job",!denied);
                boolean failed=false;try{client.runMeteredFeature("export",1,"boundary-job",()->{calls[0]++;});}catch(Exception error){failed=true;}
                if(failed!=denied||calls[0]!=(recover||denied?0:1))throw new Exception("SDK executed outside authorization or repeated business");
                if(recover&&denied){client.runMeteredFeature("export",1,"replacement-job",()->{calls[0]++;});if(calls[0]!=1)throw new Exception("cancellation did not release quota");}
                System.out.println("Java: execution boundary regression passed");return;
            }
            if(Arrays.asList(args).contains("--metering-expired-cancel-test") || Arrays.asList(args).contains("--metering-expired-commit-test")) {
                boolean cancel=Arrays.asList(args).contains("--metering-expired-cancel-test"),recover=Arrays.asList(args).contains("--metering-recover-test");int[] calls={0};
                if(!recover){
                    client.activate();
                    expectFailure(()->client.runMeteredFeature("export",1,"expired-job",()->{calls[0]++;if(cancel)throw new Exception("expected business failure");}));
                    if(calls[0]!=1)throw new Exception("business outcome was lost");
                }else if(cancel){
                    for(int i=0;i<2;i++)client.resolveMeteredFeature("expired-job",false);
                    expectFailure(()->client.resolveMeteredFeature("expired-job",true));
                    expectFailure(()->client.runMeteredFeature("export",1,"expired-job",()->{calls[0]++;}));
                    if(calls[0]!=0)throw new Exception("canceled business repeated");
                    client.runMeteredFeature("export",1,"replacement-job",()->{calls[0]++;});
                    if(calls[0]!=1)throw new Exception("expired cancellation did not release quota");
                }else{
                    expectFailure(()->client.runMeteredFeature("export",1,"expired-job",()->{calls[0]++;}));
                    expectFailure(()->client.resolveMeteredFeature("expired-job",true));
                    expectFailure(()->client.resolveMeteredFeature("expired-job",false));
                    if(calls[0]!=0)throw new Exception("completed business repeated");
                }
                System.out.println("Java: expired task regression passed");return;
            }
            if(Arrays.asList(args).stream().anyMatch(arg->arg.startsWith("--metering-"))) {
                client.activate();int[] calls={0};
                if(Arrays.asList(args).contains("--metering-jobs-test")) {
                    for(String job:new String[]{"ExportJob","exportjob","ExportJob"})client.runMeteredFeature("export",1,job,()->{calls[0]++;});
                    if(calls[0]!=2)throw new Exception("distinct jobs were skipped or repeated");
                } else if(Arrays.asList(args).contains("--metering-features-test")) {
                    for(String feature:new String[]{"export","Export"})if(Json.number(client.consume(feature,1,feature+"-job"),"used")!=1)throw new Exception("feature meters were combined");
                } else {
                    client.reserve("export",1,"pending-job");boolean failed=false;
                    try{client.runMeteredFeature("export",1,"pending-job",()->{calls[0]++;});}catch(Exception error){failed=true;}
                    if(!failed||calls[0]!=0)throw new Exception("pending job bypassed current authorization");
                }
                System.out.println("Java: metering regression passed");return;
            }
            if(Arrays.asList(args).contains("--improvements-test")) {
                client.start(null);var one=client.reserve("export",1,"cancel-job");if(!one.get("status").equals("pending")||Json.number(Json.object(one.get("consumption")),"used")!=0||Json.number(Json.object(one.get("consumption")),"reserved")!=1)throw new Exception("reserve failed");
                var canceled=client.cancel(Json.text(one,"reservation_id"),"cancel-job");if(!canceled.get("status").equals("canceled")||Json.number(Json.object(canceled.get("consumption")),"used")!=0)throw new Exception("cancel charged usage");
                var two=client.reserve("export",1,"commit-job");for(int i=0;i<2;i++){var done=client.commit(Json.text(two,"reservation_id"),"commit-job");if(!done.get("status").equals("committed")||Json.number(Json.object(done.get("consumption")),"used")!=1)throw new Exception("duplicate commit");}
                if(!client.reserve("export",1,"commit-job").get("status").equals("committed"))throw new Exception("unsafe business replay");boolean failed=false;try{client.consume("export",1000,"quota-job");}catch(Client.APIException e){failed=e.code.equals("FEATURE_QUOTA_EXCEEDED")&&!e.requestID.isEmpty()&&!e.retryable;}
                if(!failed||!client.status().get("state").equals("quota_exhausted")||!client.checkFeature("export"))throw new Exception("typed quota status missing");client.close();cfg.put("license_key","");try(Client restored=new Client(cfg)){restored.start(null);restored.validate();if(!restored.checkFeature("export"))throw new Exception("normal credential not retained");restored.deactivate();if(Boolean.TRUE.equals(restored.status().get("allowed")))throw new Exception("release grants access");}System.out.println("Java: credential / reservation / typed status OK");return;
            }
            if(Arrays.asList(args).contains("--startup-test")){var refreshed=new java.util.concurrent.CountDownLatch(1);client.onStatusChanged(status->{if("active".equals(status.get("state")))refreshed.countDown();});long start=System.nanoTime();client.start(null);if(System.nanoTime()-start>300000000L)throw new Exception("cached startup blocked");Thread.sleep(100);start=System.nanoTime();if(!client.checkFeature("export")||System.nanoTime()-start>300000000L)throw new Exception("local check blocked");if(!refreshed.await(5,java.util.concurrent.TimeUnit.SECONDS))throw new Exception("background refresh did not finish");System.out.println("Java: cached startup and local feature checks stay responsive");return;}
            if(Arrays.asList(args).contains("--offline-export")) {
                Files.writeString(Path.of(path+".request.json"),client.offlineRequest("activate"));System.out.println("Java: offline request exported");return;
            }
            if(Arrays.asList(args).contains("--transfer-test")||Arrays.asList(args).contains("--transfer-import")) {
                if(Arrays.asList(args).contains("--transfer-import"))client.importOffline(Files.readString(Path.of(path+".request.json")),Files.readString(Path.of(path+".response.json")));else client.start(null);
                client.validate();client.heartbeat();if(!client.checkFeature("export"))throw new Exception("feature denied");System.out.println("Java: transferred device credential OK");return;
            }
            if(Arrays.asList(args).contains("--floating-test")) {
                client.start(null);try(Client other=new Client(cfg)){
                    if(other.checkFeature("export"))throw new Exception("session cache reused");
                    other.start(null);
                    client.close();other.heartbeat();
                    if(!other.checkFeature("export"))throw new Exception("new session denied");
                }System.out.println("Java: floating seat release OK");return;
            }
            if(Arrays.asList(args).contains("--advanced-test")||Arrays.asList(args).contains("--offline-import")) {
                if(Arrays.asList(args).contains("--offline-import"))client.importOffline(Files.readString(Path.of(path+".request.json")),Files.readString(Path.of(path+".response.json")));else client.start(null);
                if(!client.checkFeature("export"))throw new Exception("feature denied");
                if(((Number)client.consume("export",1,"business-job-1").get("used")).longValue()!=1||((Number)client.consume("export",1,"business-job-1").get("used")).longValue()!=1)throw new Exception("consume replay failed");
                var over=client.consume("export",3,"business-job-2");if(((Number)over.get("used")).longValue()!=4||((Number)over.get("overage")).longValue()!=1)throw new Exception("overage failed");
                boolean failed=false;try{client.consume("export",1,"business-job-3");}catch(Client.APIException e){failed=e.code.equals("FEATURE_QUOTA_EXCEEDED");}
                if(!failed||!client.checkFeature("export"))throw new Exception("quota denial failed");client.deactivate();System.out.println("Java: offline/metered feature checks OK");return;
            }
            if(Arrays.asList(args).contains("--refresh-test")) {
                boolean failed=false;try{client.refreshOnline();}catch(Exception e){failed=true;}
                if(!failed||!client.checkFeature("export"))throw new Exception("refresh failure did not retain offline access");
                client.refreshOnline();if(!client.checkFeature("updated")||client.checkFeature("export"))throw new Exception("online refresh did not replace cached policy");
                failed=false;try{client.refreshOnline();}catch(Exception e){failed=true;}
                if(!failed||client.checkFeature("updated"))throw new Exception("refresh denial did not clear cache");
                System.out.println("Java: online refresh OK");return;
            }
            if(Arrays.asList(args).contains("--policy-test")) {
                boolean deny=Arrays.asList(args).contains("--expect-denied");if(client.checkFeature("export")==deny)throw new Exception("unexpected cache device/policy access");
                if(!deny){client.activate();client.validate();client.heartbeat();}System.out.println("Java: policy cache OK");return;
            }
            var claims = client.activate();
            System.out.println("Java: activated " + claims.get("activation_id"));
            if (!cacheTest) {
                client.validate(); client.heartbeat();
                System.out.println("Java: export enabled " + client.checkFeature("export"));
                client.deactivate();
                System.out.println("Java: deactivated");
                return;
            }
        }
        try (Client restored = new Client(cfg)) {
            restored.validate();
            if (!restored.checkFeature("export")) throw new Exception("cache not restored during server outage");
            boolean rejected = false;
            try { restored.validate(); } catch (Client.APIException e) { rejected = e.status == 403; }
            if (!rejected || restored.checkFeature("export")) throw new Exception("rejection did not invalidate cache");
        }
        try (Client reopened = new Client(cfg)) {
            if (reopened.checkFeature("export")) throw new Exception("rejected cache survived restart");
        }
        System.out.println("Java: cache restored on 503; 403 invalidated access and persisted cache");
    }
}
