package licentivo;

import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.MessageDigest;
import java.util.*;
import java.util.concurrent.TimeUnit;
import java.util.regex.*;

/** Binding uses OS identity, never a portable config/cache identifier. */
public final class MachineIdentity {
    private MachineIdentity() {}
    private static String command(String... args) throws Exception {
        Process process=new ProcessBuilder(args).redirectErrorStream(true).start();
        if(!process.waitFor(5,TimeUnit.SECONDS)){process.destroyForcibly();throw new IllegalStateException("machine identity lookup timed out");}
        if(process.exitValue()!=0)throw new IllegalStateException("OS machine identity unavailable");
        return new String(process.getInputStream().readNBytes(65536),StandardCharsets.UTF_8);
    }
    private static String systemID(String product) throws Exception {
        String os=System.getProperty("os.name").toLowerCase(Locale.ROOT),identity="";
        if(os.startsWith("windows")){
            Matcher m=Pattern.compile("MachineGuid\\s+REG_SZ\\s+([0-9a-f-]+)",Pattern.CASE_INSENSITIVE).matcher(command("reg.exe","query","HKLM\\SOFTWARE\\Microsoft\\Cryptography","/v","MachineGuid","/reg:64"));
            if(m.find())identity=m.group(1);
        }else if(os.contains("linux")){
            for(String file:new String[]{"/etc/machine-id","/var/lib/dbus/machine-id"}){try{identity=Files.readString(Path.of(file)).trim();}catch(Exception ignored){}if(!identity.isEmpty())break;}
        }else if(os.contains("mac")){
            Matcher m=Pattern.compile("\"IOPlatformUUID\"\\s*=\\s*\"([0-9A-Fa-f-]+)\"").matcher(command("/usr/sbin/ioreg","-rd1","-c","IOPlatformExpertDevice"));if(m.find())identity=m.group(1);
        }
        identity=identity.trim().toLowerCase(Locale.ROOT);
        if(!identity.matches("(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})")||identity.replace("-","").replace("0","").isEmpty())throw new IllegalStateException("OS machine identity unavailable; cannot bind license");
        return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(("LicenovaDevice/v2\n"+product.toLowerCase(Locale.ROOT)+"\n"+identity).getBytes(StandardCharsets.UTF_8)));
    }

    private static String hash(String value)throws Exception{return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8)));}
    public static Map<String,Object> profile(String product)throws Exception{
        String system=systemID(product),hardware="",description="",os=System.getProperty("os.name").toLowerCase(Locale.ROOT),platform="";
        try{
            if(os.startsWith("windows")){platform="windows";String result=command("powershell.exe","-NoProfile","-NonInteractive","-Command","(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model");hardware=result.trim().split("\\r?\\n")[0];description=result;}
            else if(os.contains("linux")){platform="linux";try{hardware=Files.readString(Path.of("/sys/class/dmi/id/product_uuid"));}catch(Exception ignored){}for(String path:new String[]{"/sys/class/dmi/id/sys_vendor","/sys/class/dmi/id/product_name"})try{description+=" "+Files.readString(Path.of(path));}catch(Exception ignored){}if(Files.exists(Path.of("/.dockerenv")))description+=" virtual machine";}
            else if(os.contains("mac")){platform="darwin";String result=command("/usr/sbin/ioreg","-rd1","-c","IOPlatformExpertDevice");Matcher match=Pattern.compile("\"IOPlatformUUID\"\\s*=\\s*\"([0-9A-Fa-f-]+)\"").matcher(result);if(match.find())hardware=match.group(1);description=result;}
        }catch(Exception ignored){}
        hardware=hardware.trim().toLowerCase(Locale.ROOT);String hw="";if(hardware.matches("[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}")&&!hardware.replace("-","").replace("0","").replace("f","").isEmpty())hw=hash("LicenovaHardware/v1\n"+product.toLowerCase(Locale.ROOT)+"\n"+hardware);
        Map<String,Object> p=new LinkedHashMap<>();p.put("version",3L);p.put("system_id",system);p.put("hardware_id",hw);p.put("virtual_machine",Pattern.compile("vmware|virtualbox|virtual machine|kvm|qemu|xen|parallels",Pattern.CASE_INSENSITIVE).matcher(description).find());p.put("platform",platform);return p;
    }
    public static String deviceID(String product,Map<String,Object>profile)throws Exception{return hash("LicenovaDevice/v3\n"+product.toLowerCase(Locale.ROOT)+"\n"+profile.get("system_id")+"\n"+profile.get("hardware_id"));}
    public static String deviceID(String product)throws Exception{return deviceID(product,profile(product));}
}
