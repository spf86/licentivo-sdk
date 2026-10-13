package licentivo;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class Demo {
    public static void main(String[] args) throws Exception {
        String path = args.length > 1 && args[0].equals("--config") ? args[1] : ".local/sdk-demo.json";
        var cfg = Json.object(Json.parse(Files.readString(Path.of(path))));
        boolean cacheTest = Arrays.asList(args).contains("--cache-test");
        try (Client client = new Client(cfg)) {
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
                    boolean failed=false;try{other.start(null);}catch(Client.APIException e){failed=e.status==409;}
                    if(!failed)throw new Exception("occupied seat granted");client.close();other.start(null);
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
