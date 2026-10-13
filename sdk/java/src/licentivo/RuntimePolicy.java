package licentivo;
import java.util.*;
final class RuntimePolicy {
    static String mode(Map<String,Object> c){return c.containsKey("runtime_policy")?Json.text(Json.object(c.get("runtime_policy")),"offline_mode"):"limited";}
    static int heartbeat(Map<String,Object> c,int fallback){return c.containsKey("runtime_policy")?(int)Json.number(Json.object(c.get("runtime_policy")),"heartbeat_seconds"):fallback;}
    static long maxSpan(Map<String,Object> c){
        if(!c.containsKey("runtime_policy"))return 86400;
        Map<String,Object>p=Json.object(c.get("runtime_policy"));if(!p.keySet().equals(Set.of("offline_mode","heartbeat_seconds","offline_seconds")))throw new IllegalArgumentException("invalid runtime policy");
        String mode=Json.text(p,"offline_mode");long h=Json.number(p,"heartbeat_seconds"),o=Json.number(p,"offline_seconds");
        if(h!=0&&(h<10||h>86400))throw new IllegalArgumentException("invalid heartbeat interval");
        if(mode.equals("permanent")&&o==0)return 253402300799L-Json.number(c,"issued_at");
        if(mode.equals("none")&&o==0)return h==0?10:h+(h+9)/10+30;
        if(mode.equals("limited")&&o>=1&&o<=31536000)return o;
        throw new IllegalArgumentException("invalid runtime policy");
    }
    static boolean fields(Map<String,Object>c){Set<String> expected=new HashSet<>(Set.of("version","key_id","tenant_id","product_id","license_id","device_id","activation_id","mode","features","max_devices","issued_at","expires_at"));if(c.containsKey("runtime_policy"))expected.add("runtime_policy");for(String f:new String[]{"capabilities","offline_request_id"})if(c.containsKey(f))expected.add(f);return c.keySet().equals(expected);}
}
