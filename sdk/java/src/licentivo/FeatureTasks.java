package licentivo;

import java.io.IOException;
import java.nio.file.*;
import java.nio.file.attribute.PosixFilePermissions;
import java.security.MessageDigest;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;
import java.time.Instant;
import java.time.Duration;

/** Private task IDs/outcome only; never business data or activation secrets. */
final class FeatureTasks {
    final AtomicBoolean busy = new AtomicBoolean();
    final Client client;
    final Path cache;
    FeatureTasks(Client client, Path cache) { this.client=client; this.cache=cache; }
    Path path(String id) throws Exception {
        if(id==null || id.isEmpty() || id.length()>80 || id.chars().anyMatch(ch->ch<33||ch>126)) throw new IllegalArgumentException("a stable operationID of 1-80 non-whitespace ASCII characters is required");
        String digest=HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest((client.taskSource()+"\n"+id).getBytes(StandardCharsets.UTF_8)));
        Path path=Path.of(cache+".task-"+digest+".json");
        String oldDigest=HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest((client.legacyTaskSource()+"\n"+id).getBytes(StandardCharsets.UTF_8)));
        Path legacy=Path.of(cache+".task-"+oldDigest+".json");
        if(!path.equals(legacy)&&!Files.exists(path)&&Files.exists(legacy))Files.move(legacy,path);
        return path;
    }
    Map<String,Object> read(Path path,String id) throws Exception {
        if(!Files.exists(path))return new LinkedHashMap<>(Map.of("version",1L,"operation_id",id,"reservation_id","","state",""));
        if(Files.size(path)>4096)throw new IOException("invalid feature task journal");
        var task=Json.object(Json.parse(Files.readString(path)));
        if(Json.number(task,"version")!=1 || !id.equals(task.get("operation_id")) || Json.text(task,"feature").isEmpty() || Json.number(task,"quantity")<=0 || Json.text(task,"reservation_id").isEmpty() || !(task.get("state") instanceof String))throw new IOException("invalid feature task journal; do not repeat business");
        return new LinkedHashMap<>(task);
    }
    void save(Path path,Map<String,Object> task) throws Exception {
        path=path.toAbsolutePath();Files.createDirectories(path.getParent());
        Path temporary=Files.createTempFile(path.getParent(),".feature-task-",".tmp");
        try{
            try{Files.setPosixFilePermissions(temporary,PosixFilePermissions.fromString("rw-------"));}catch(UnsupportedOperationException ignored){}
            Files.writeString(temporary,Json.write(task));
            try{Files.move(temporary,path,StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException error){Files.move(temporary,path,StandardCopyOption.REPLACE_EXISTING);}
        }finally{Files.deleteIfExists(temporary);}
    }
    void finish(Path path,Map<String,Object> task,boolean succeeded) throws Exception {
        Map<String,Object> receipt;
        try{receipt=succeeded?client.commit(Json.text(task,"reservation_id"),Json.text(task,"operation_id")):client.cancel(Json.text(task,"reservation_id"),Json.text(task,"operation_id"));}
        catch(Exception error){throw new IOException("confirmation pending for operation "+task.get("operation_id")+"; do not repeat business",error);}
        String expected=succeeded?"committed":"canceled";
        if(!expected.equals(receipt.get("status")) && (succeeded || !"expired".equals(receipt.get("status"))))throw new IllegalStateException("reservation closed without requested outcome; do not repeat business");
        task.put("state",expected);save(path,task);
    }
    final class Renewal implements AutoCloseable {
        final Thread worker;
        volatile boolean stopped;
        Renewal(String feature,long quantity,String id,Map<String,Object> hold) {
            worker=new Thread(()->{
                Instant until=Instant.parse(Json.text(hold,"expires_at"));long interval=60000;
                while(!stopped){
                    long remaining=Duration.between(Instant.now(),until).toMillis();
                    if(remaining<=0)return;
                    try{Thread.sleep(Math.min(interval,Math.max(10,remaining/3)));}
                    catch(InterruptedException error){return;}
                    if(stopped)return;
                    try{
                        var renewed=client.reserve(feature,quantity,id);
                        if(!"pending".equals(renewed.get("status"))||!hold.get("reservation_id").equals(renewed.get("reservation_id"))||!id.equals(renewed.get("operation_id")))return;
                        until=Instant.parse(Json.text(renewed,"expires_at"));interval=60000;
                    }catch(Exception error){if(stopped)return;interval=5000;}
                }
            },"licentivo-reservation");
            worker.setDaemon(true);worker.start();
        }
        public void close(){
            stopped=true;worker.interrupt();boolean interrupted=false;
            while(worker.isAlive())try{worker.join();}catch(InterruptedException error){interrupted=true;}
            if(interrupted)Thread.currentThread().interrupt();
        }
    }
    void run(String feature,long quantity,String id,Client.FeatureAction action) throws Exception {
        Objects.requireNonNull(action,"business action is required");
        if(quantity<=0)throw new IllegalArgumentException("positive quantity is required");
        if(!busy.compareAndSet(false,true))throw new IllegalStateException("another feature task is running");
        try{
            Path path=path(id);var task=read(path,id);String state=Json.text(task,"state");
            if(!state.isEmpty() && (!feature.equals(task.get("feature")) || quantity!=Json.number(task,"quantity")))throw new IllegalStateException("operation ID was already used for another feature or quantity");
            task.put("feature",feature);task.put("quantity",quantity);
            if(state.equals("committed"))return;
            if(state.equals("commit_pending")){finish(path,task,true);return;}
            if(state.equals("cancel_pending")){finish(path,task,false);state="canceled";}
            if(state.equals("canceled"))throw new IllegalStateException("operation canceled; use a new job ID for a new action");
            if(state.equals("business_running"))throw new IllegalStateException("business outcome unknown; verify its result and call resolveMeteredFeature");
            if(!state.isEmpty())throw new IllegalStateException("invalid feature task state");
            client.runFeature(feature,()->{});
            var hold=client.reserve(feature,quantity,id);
            if("committed".equals(hold.get("status"))){task.put("reservation_id",hold.get("reservation_id"));task.put("state","committed");save(path,task);return;}
            if(!"pending".equals(hold.get("status")))throw new IllegalStateException("reservation is closed; business was not run");
            task.put("reservation_id",hold.get("reservation_id"));task.put("state","business_running");
            try{save(path,task);}catch(Exception error){try{client.cancel(Json.text(task,"reservation_id"),id);}catch(Exception ignored){}throw error;}
            final Renewal[] renewal={null};
            try{
            try{client.runFeature(feature,()->{
                if(!java.time.Instant.parse(Json.text(hold,"expires_at")).isAfter(java.time.Instant.now()))throw new IllegalStateException("reservation expired; business was not run");
                renewal[0]=new Renewal(feature,quantity,id,hold);
                action.run();
            });}catch(Exception error){task.put("state","cancel_pending");try{save(path,task);if(renewal[0]!=null)renewal[0].close();finish(path,task,false);}catch(Exception ignored){}throw error;}
            task.put("state","commit_pending");
            try{save(path,task);}catch(Exception error){throw new IOException("business completed; journal update failed; do not repeat operation "+id,error);}
            if(renewal[0]!=null)renewal[0].close();
            finish(path,task,true);
            }finally{if(renewal[0]!=null)renewal[0].close();}
        }finally{busy.set(false);}
    }
    void resolve(String id,boolean succeeded) throws Exception {
        if(!busy.compareAndSet(false,true))throw new IllegalStateException("another feature task is running");
        try{
            Path path=path(id);var task=read(path,id);String state=Json.text(task,"state");
            if(state.equals(succeeded?"committed":"canceled"))return;
            if(Json.text(task,"reservation_id").isEmpty() || !Set.of("business_running","commit_pending","cancel_pending").contains(state) || state.equals("commit_pending")&&!succeeded || state.equals("cancel_pending")&&succeeded)throw new IllegalStateException("task cannot be resolved with that outcome");
            task.put("state",succeeded?"commit_pending":"cancel_pending");save(path,task);finish(path,task,succeeded);
        }finally{busy.set(false);}
    }
}
