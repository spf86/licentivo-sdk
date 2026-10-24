package licentivo;

import java.io.*;
import java.net.URI;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.nio.file.attribute.PosixFilePermissions;
import java.nio.channels.FileChannel;
import java.security.MessageDigest;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.*;

/** Durable unlink capabilities contain no runtime credential or login assertion. */
final class LogoutJournal {
    private final Map<String,Object> cfg;
    private final Path path;
    private Map<String,Object> state=new LinkedHashMap<>();
    private boolean corrupt;
    private final Set<String> blockedBindings=new HashSet<>();
    LogoutJournal(Map<String,Object> config){
        cfg=config;path=Path.of(Json.text(cfg,"cache_path")+".logout.json");state.put("version",1L);state.put("source_hash","");state.put("binding_key","");state.put("signed_out",false);state.put("pending",new ArrayList<>());
        try{if(Files.size(path)>65536)throw new IOException("invalid logout journal");Map<String,Object> j=Json.object(Json.parse(Files.readString(path),65536));if(Json.number(j,"version")!=1||!(j.get("pending") instanceof List<?>))throw new IOException("invalid logout journal");state=j;}catch(NoSuchFileException ignored){}catch(Exception error){corrupt=true;}
    }
    private static String digest(String s)throws Exception{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(s.getBytes(StandardCharsets.UTF_8)));}
    private Path directory()throws Exception{
        String root=System.getenv("LOCALAPPDATA");if(root==null||root.isEmpty())root=System.getenv("XDG_CACHE_HOME");if(root==null||root.isEmpty())root=Path.of(System.getProperty("user.home"),".cache").toString();
        return Path.of(root,"Licentivo","logout",digest(Path.of(Json.text(cfg,"cache_path")).toAbsolutePath().normalize()+"\n"+Json.text(cfg,"base_url")+"\n"+Json.text(cfg,"product_id")+"\n"+Json.text(cfg,"device_id")));
    }
    static String binding(Map<String,Object> ticket){return ticket==null?"":Json.text(ticket,"activation_id")+":"+Json.number(ticket,"expected_generation");}
    private static String optional(Map<String,Object> j,String key){return j.get(key) instanceof String?(String)j.get(key):"";}
    private static String scope(Map<String,Object> j){return optional(j,"source_hash")+"\n"+optional(j,"binding_key");}
    private static long revision(Map<String,Object> j){return j.get("revision") instanceof Number?((Number)j.get("revision")).longValue():0;}
    boolean blocked(String sourceHash,Map<String,Object> ticket){
        Map<String,Map<String,Object>> records=new LinkedHashMap<>();records.put(scope(state),state);
        try{Path dir=directory();if(Files.exists(dir))try(var paths=Files.list(dir)){for(Path p:paths.filter(p->p.toString().endsWith(".json")).toList()){
            if(Files.size(p)>65536)throw new IOException("invalid logout journal");Map<String,Object> j=Json.object(Json.parse(Files.readString(p),65536));if(Json.number(j,"version")!=1||!(j.get("pending") instanceof List<?>))throw new IOException("invalid logout journal");Map<String,Object> old=records.get(scope(j));if(old==null||revision(j)>revision(old)||revision(j)==revision(old)&&(Boolean.TRUE.equals(j.get("signed_out"))||!Boolean.TRUE.equals(old.get("signed_out"))))records.put(scope(j),j);
        }}}catch(Exception error){corrupt=true;}
        boolean denied=corrupt;String key=binding(ticket);Map<String,Object> tickets=new LinkedHashMap<>();
        for(Map<String,Object> j:records.values()){
            String b=optional(j,"binding_key");if(Boolean.TRUE.equals(j.get("signed_out"))&&!b.isEmpty())blockedBindings.add(b);if((sourceHash.equals(Json.text(j,"source_hash"))||!optional(cfg,"launch_connection_id").isEmpty()&&!key.isEmpty()&&key.equals(b)||sourceHash.isEmpty()&&optional(cfg,"identity_token").isEmpty()&&Json.text(cfg,"license_key").isEmpty())&&(b.isEmpty()||key.isEmpty()||b.equals(key))){state=j;if(Boolean.TRUE.equals(j.get("signed_out"))&&Json.text(cfg,"license_key").isEmpty())denied=true;}
            for(Object t:(List<?>)j.get("pending"))tickets.put(Json.text(Json.object(t),"operation_id"),t);
        }
        state.put("pending",new ArrayList<>(tickets.values()));return denied;
    }
    @SuppressWarnings("unchecked") private List<Object> pending(){return (List<Object>)state.get("pending");}
    boolean hasPending(){return !pending().isEmpty();}
    boolean bindingBlocked(Map<String,Object> result){return !optional(cfg,"launch_connection_id").isEmpty()&&result.get("logout_ticket") instanceof Map&&blockedBindings.contains(binding(Json.object(result.get("logout_ticket"))));}
    void acceptLogin()throws Exception{if(Boolean.TRUE.equals(state.get("signed_out"))){state.put("signed_out",false);save();}}
    void deny(String sourceHash,Map<String,Object> ticket)throws Exception{
        if(!sourceHash.isEmpty())state.put("source_hash",sourceHash);state.put("signed_out",true);
        if(ticket!=null){state.put("binding_key",binding(ticket));blockedBindings.add(binding(ticket));}
        if(ticket!=null&&pending().stream().noneMatch(p->Objects.equals(Json.object(p).get("operation_id"),ticket.get("operation_id"))))pending().add(new LinkedHashMap<>(ticket));save();
    }
    void retry(HttpClient http)throws Exception{
        while(hasPending()){
            HttpRequest request=HttpRequest.newBuilder(URI.create(Json.text(cfg,"base_url")+"/api/v1/sessions/logout")).timeout(Duration.ofSeconds(5)).header("Content-Type","application/json").POST(HttpRequest.BodyPublishers.ofString(Json.write(pending().get(0)))).build();
            CompletableFuture<HttpResponse<byte[]>> pending=http.sendAsync(request,info->new Client.BoundedBody());HttpResponse<byte[]> response;
            try{response=pending.get(5,TimeUnit.SECONDS);}catch(Exception error){pending.cancel(true);throw error;}byte[] raw=response.body();
            if(response.statusCode()!=200||raw.length>65536)throw new IOException("device revocation pending");
            Map<String,Object> envelope=Json.object(Json.parse(new String(raw,StandardCharsets.UTF_8))),ack=Json.object(envelope.get("data")),ticket=Json.object(pending().get(0));if(!Set.of("succeeded","stale").contains(Json.text(ack,"status"))||!Objects.equals(ack.get("operation_id"),ticket.get("operation_id"))||!Objects.equals(ack.get("activation_id"),ticket.get("activation_id"))||Json.number(ack,"expected_generation")!=Json.number(ticket,"expected_generation"))throw new IOException("device revocation pending");
            pending().remove(0);acknowledge(Json.text(ticket,"operation_id"));save();
        }
    }
    private void save()throws Exception{
        state.put("revision",Math.max(revision(state)+1,System.currentTimeMillis()));
        Exception fallback=null,primary=null;try{write(directory().resolve(digest(scope(state))+".json"));}catch(Exception error){fallback=error;}try{write(path);}catch(Exception error){primary=error;}
        if(fallback!=null&&(!Boolean.TRUE.equals(state.get("signed_out"))||primary!=null))throw fallback;
    }
    private void write(Path target)throws Exception{
        write(target,state);
    }
    private void acknowledge(String operation)throws Exception{
        Path dir=directory();if(!Files.exists(dir))return;try(var paths=Files.list(dir)){for(Path p:paths.filter(p->p.toString().endsWith(".json")).toList()){
            if(Files.size(p)>65536)throw new IOException("invalid logout journal");Map<String,Object> j=Json.object(Json.parse(Files.readString(p),65536));List<?> old=(List<?>)j.get("pending");List<?> remaining=old.stream().filter(t->!operation.equals(Json.text(Json.object(t),"operation_id"))).toList();if(remaining.size()!=old.size()){j.put("pending",remaining);write(p,j);}
        }}
    }
    private void write(Path target,Map<String,Object> journal)throws Exception{
        Path absolute=target.toAbsolutePath();Files.createDirectories(absolute.getParent());Path tmp=Files.createTempFile(absolute.getParent(),".logout-",".tmp");
        try{try{Files.setPosixFilePermissions(tmp,PosixFilePermissions.fromString("rw-------"));}catch(UnsupportedOperationException ignored){}Files.writeString(tmp,Json.write(journal));try(var file=FileChannel.open(tmp,StandardOpenOption.WRITE)){file.force(true);}try{Files.move(tmp,absolute,StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException error){Files.move(tmp,absolute,StandardCopyOption.REPLACE_EXISTING);}}finally{Files.deleteIfExists(tmp);}
    }
}
