package licentivo;
import java.util.*;
final class Capabilities {
 static void verify(Map<String,Object> c){
  if(c.containsKey("offline_request_id")&&!Json.text(c,"offline_request_id").matches("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}"))throw new IllegalArgumentException("invalid offline request ID");
  if(!c.containsKey("capabilities"))return;Map<String,Object>x=Json.object(c.get("capabilities"));Set<String>names=Set.of("license_model","session_id","session_expires_at","app_version","version_released_at","maintenance_until");
  if(!names.containsAll(x.keySet())||!Set.of("node_locked","floating").contains(Json.text(x,"license_model")))throw new IllegalArgumentException("invalid license capabilities");
  for(String field:new String[]{"session_expires_at","version_released_at","maintenance_until"})if(x.containsKey(field)&&(Json.number(x,field)<0||Json.number(x,field)>253402300799L))throw new IllegalArgumentException("invalid entitlement date");
  if(x.containsKey("app_version")&&!Json.text(x,"app_version").matches("(0|[1-9][0-9]{0,8})\\.(0|[1-9][0-9]{0,8})\\.(0|[1-9][0-9]{0,8})"))throw new IllegalArgumentException("invalid application version");
  if(Json.text(x,"license_model").equals("floating")&&(!Json.text(x,"session_id").matches("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}")||number(x,"session_expires_at")<=Json.number(c,"issued_at")||Json.number(c,"expires_at")>number(x,"session_expires_at")||!c.containsKey("runtime_policy")||RuntimePolicy.heartbeat(c,0)==0||RuntimePolicy.mode(c).equals("permanent")))throw new IllegalArgumentException("invalid floating session");
  if(number(x,"version_released_at")>Json.number(c,"issued_at")||number(x,"maintenance_until")>0&&(!x.containsKey("app_version")||number(x,"version_released_at")==0||number(x,"version_released_at")>number(x,"maintenance_until")))throw new IllegalArgumentException("invalid maintenance entitlement");
 }
 static long number(Map<String,Object>x,String field){return x.containsKey(field)?Json.number(x,field):0;}
 static boolean floating(Map<String,Object>c){return c.containsKey("capabilities")&&"floating".equals(Json.object(c.get("capabilities")).get("license_model"));}
 static void context(Map<String,Object>c,Map<String,Object>cfg,String session){if(!c.containsKey("capabilities"))return;Map<String,Object>x=Json.object(c.get("capabilities"));if(!x.getOrDefault("app_version","").equals(cfg.getOrDefault("app_version",""))||floating(c)&&!session.equals(x.get("session_id")))throw new IllegalArgumentException("lease belongs to another version/session");}
}
