package licentivo;

import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.nio.file.attribute.PosixFilePermissions;
import java.security.MessageDigest;
import java.util.*;

/** Retain device authentication separately from expiring signed leases. */
final class DeviceCredential {
    private final Map<String,Object> cfg;
    private Map<String,Object> state;
    private boolean dirty;
    DeviceCredential(Map<String,Object> config)throws Exception {
        cfg=config;
        if(path()==null)return;
        try {
            Path p=path();if(Files.size(p)>16384)return;
            Map<String,Object>s=Json.object(Json.parse(Files.readString(p)));
            if(Json.number(s,"version")!=1||!Objects.equals(cfg.get("base_url"),s.get("origin"))||!Json.text(s,"source_hash").matches("[0-9a-f]{64}")||(!Json.text(cfg,"license_key").isEmpty()||!Json.text(cfg,"identity_token").isEmpty())&&!hash().equals(s.get("source_hash"))||!Objects.equals(cfg.get("product_id"),s.get("product_id"))||!Objects.equals(cfg.get("device_id"),s.get("device_id"))||!uuid(Json.text(s,"request_id"))||!uuid(Json.text(s,"session_id")))return;
            String key=Json.text(s,"device_key");if(!key.isEmpty()&&!key.matches("lv_dev_[A-Za-z0-9_-]{43}"))return;
            if(Json.text(cfg,"license_key").isEmpty()&&key.isEmpty())return;
            state=s;
        }catch(Exception ignored){}
    }
    boolean temporary(){return !Json.text(cfg,"license_key").isEmpty()||!Json.text(cfg,"identity_token").isEmpty()||state!=null;}
    String cacheHash()throws Exception{return !Json.text(cfg,"identity_token").isEmpty()||!Json.text(cfg,"license_key").isEmpty()?hash():state!=null?Json.text(state,"source_hash"):"";}
    String key(){return state!=null&&!Json.text(state,"device_key").isEmpty()?Json.text(state,"device_key"):Json.text(cfg,"license_key");}
    String session(String current){return state!=null&&Json.text(state,"device_key").isEmpty()?Json.text(state,"session_id"):current;}
    String request(String current){return state!=null&&Json.text(state,"device_key").isEmpty()?Json.text(state,"request_id"):current;}
    private static boolean uuid(String s){return s.matches("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}");}
    private String hash()throws Exception{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest((Json.text(cfg,"identity_token").isEmpty()?Json.text(cfg,"license_key"):Json.text(cfg,"identity_token")).getBytes(StandardCharsets.UTF_8)));}
    private Path path(){Object p=cfg.get("cache_path");return p instanceof String&&!((String)p).isEmpty()?Path.of((String)p+".credential.json"):null;}
    void prepare(String session)throws Exception {
        if(!temporary())return;
        if(path()==null)throw new IllegalArgumentException("cache_path required to retain a transferred license");
        if(state==null){state=new LinkedHashMap<>();state.put("version",1L);state.put("origin",cfg.get("base_url"));state.put("source_hash",hash());state.put("product_id",cfg.get("product_id"));state.put("device_id",cfg.get("device_id"));state.put("device_key","");state.put("request_id",UUID.randomUUID().toString());state.put("session_id",session);dirty=true;}
        if(dirty)save();
    }
    void accept(Map<String,Object> result,String session)throws Exception {
        if(result.get("logout_ticket") instanceof Map){prepare(session);state.put("logout_ticket",result.get("logout_ticket"));dirty=true;save();}if(!temporary()||state!=null&&!Json.text(state,"device_key").isEmpty())return;
        String key=(String)result.getOrDefault("device_key","");if(!key.matches("lv_dev_[A-Za-z0-9_-]{43}")){if(!Json.text(cfg,"license_key").startsWith("lv_tmp_")&&Json.text(cfg,"identity_token").isEmpty())return;throw new IllegalArgumentException("server omitted device credential");}
        prepare(session);state.put("device_key",key);dirty=true;save();
    }
    void captureLogout(Map<String,Object> result,String session)throws Exception{if(result.get("logout_ticket") instanceof Map){prepare(session);state.put("logout_ticket",result.get("logout_ticket"));}}
    Map<String,Object> logoutTicket(){return state!=null&&state.get("logout_ticket") instanceof Map?Json.object(state.get("logout_ticket")):null;}
    void clear(){state=null;dirty=false;}
    private void save()throws Exception {
        Path p=path().toAbsolutePath();Files.createDirectories(p.getParent());Path tmp=Files.createTempFile(p.getParent(),".credential-",".tmp");
        try{try{Files.setPosixFilePermissions(tmp,PosixFilePermissions.fromString("rw-------"));}catch(UnsupportedOperationException ignored){}Files.writeString(tmp,Json.write(state));try{Files.move(tmp,p,StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException e){Files.move(tmp,p,StandardCopyOption.REPLACE_EXISTING);}dirty=false;}finally{Files.deleteIfExists(tmp);}
    }
}
