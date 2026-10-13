package licentivo;

import java.io.IOException;
import java.nio.file.*;
import java.nio.file.attribute.PosixFilePermissions;
import java.security.MessageDigest;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;

/** Private task IDs/outcome only; never business data or activation secrets. */
final class FeatureTasks {
    final AtomicBoolean busy = new AtomicBoolean();
    final Client client;
    final Path cache;
    final String licenseHash;
    FeatureTasks(Client client, Path cache, String licenseHash) { this.client=client; this.cache=cache; this.licenseHash=licenseHash; }
    Path path(String id) throws Exception {
        if(id==null || id.isEmpty() || id.length()>80 || id.chars().anyMatch(ch->ch<33||ch>126)) throw new IllegalArgumentException("a stable operationID of 1-80 non-whitespace ASCII characters is required");
        String digest=HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest((licenseHash+"\n"+id).getBytes(StandardCharsets.UTF_8)));
        return Path.of(cache+".task-"+digest+".json");
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
        if(!expected.equals(receipt.get("status")))throw new IllegalStateException("reservation closed without requested outcome; do not repeat business");
        task.put("state",expected);save(path,task);
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
            try{action.run();}catch(Exception error){task.put("state","cancel_pending");try{save(path,task);finish(path,task,false);}catch(Exception ignored){}throw error;}
            task.put("state","commit_pending");
            try{save(path,task);}catch(Exception error){throw new IOException("business completed; journal update failed; do not repeat operation "+id,error);}
            finish(path,task,true);
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
