using System.Diagnostics;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;
using Microsoft.Win32;
namespace Licentivo;
public static class MachineIdentity {
    private static string SystemId(string product){
        string identity="";
        if(OperatingSystem.IsWindows()){
            using var hive=RegistryKey.OpenBaseKey(RegistryHive.LocalMachine,RegistryView.Registry64);
            using var key=hive.OpenSubKey(@"SOFTWARE\Microsoft\Cryptography");identity=key?.GetValue("MachineGuid") as string??"";
        }else if(OperatingSystem.IsLinux()){
            foreach(var path in new[]{"/etc/machine-id","/var/lib/dbus/machine-id"}){try{identity=File.ReadAllText(path).Trim();}catch(IOException){}if(identity!="")break;}
        }else if(OperatingSystem.IsMacOS()){
            using var process=Process.Start(new ProcessStartInfo("/usr/sbin/ioreg"){ArgumentList={"-rd1","-c","IOPlatformExpertDevice"},RedirectStandardOutput=true,UseShellExecute=false,CreateNoWindow=true})??throw new InvalidOperationException("OS identity unavailable");
            if(!process.WaitForExit(3000)){process.Kill();throw new InvalidOperationException("machine identity lookup timed out");}
            identity=Regex.Match(process.StandardOutput.ReadToEnd(),"\"IOPlatformUUID\"\\s*=\\s*\"([0-9A-Fa-f-]+)\"").Groups[1].Value;
        }
        identity=identity.Trim().ToLowerInvariant();
        if(!Regex.IsMatch(identity,"^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$")||identity.Replace("-","").Replace("0","")=="")throw new InvalidOperationException("OS machine identity unavailable; cannot bind license");
        return Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes("LicenovaDevice/v2\n"+product.ToLowerInvariant()+"\n"+identity))).ToLowerInvariant();
    }

    private static string Hash(string value)=>Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(value))).ToLowerInvariant();
    public static DeviceProfile Profile(string product){
        string system=SystemId(product),hardware="",description="",platform=OperatingSystem.IsWindows()?"windows":OperatingSystem.IsLinux()?"linux":"darwin";
        try{
            if(OperatingSystem.IsWindows()){
                var start=new ProcessStartInfo("powershell.exe"){ArgumentList={"-NoProfile","-NonInteractive","-Command","(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model"},RedirectStandardOutput=true,UseShellExecute=false,CreateNoWindow=true};
                using var process=Process.Start(start)??throw new IOException("hardware lookup unavailable");if(!process.WaitForExit(5000)){process.Kill();throw new IOException("hardware lookup timed out");}description=process.StandardOutput.ReadToEnd();hardware=description.Trim().Split('\n')[0];
            }else if(OperatingSystem.IsLinux()){
                try{hardware=File.ReadAllText("/sys/class/dmi/id/product_uuid");}catch(IOException){}foreach(string path in new[]{"/sys/class/dmi/id/sys_vendor","/sys/class/dmi/id/product_name"})try{description+=" "+File.ReadAllText(path);}catch(IOException){}if(File.Exists("/.dockerenv"))description+=" virtual machine";
            }else if(OperatingSystem.IsMacOS()){
                using var process=Process.Start(new ProcessStartInfo("/usr/sbin/ioreg"){ArgumentList={"-rd1","-c","IOPlatformExpertDevice"},RedirectStandardOutput=true,UseShellExecute=false,CreateNoWindow=true})??throw new IOException("hardware lookup unavailable");if(!process.WaitForExit(5000)){process.Kill();throw new IOException("hardware lookup timed out");}description=process.StandardOutput.ReadToEnd();hardware=Regex.Match(description,"\"IOPlatformUUID\"\\s*=\\s*\"([0-9A-Fa-f-]+)\"").Groups[1].Value;
            }
        }catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.ComponentModel.Win32Exception){}
        hardware=hardware.Trim().ToLowerInvariant();string hw="";if(Guid.TryParseExact(hardware,"D",out _)&&hardware.Replace("-","").Replace("0","").Replace("f","")!="")hw=Hash("LicenovaHardware/v1\n"+product.ToLowerInvariant()+"\n"+hardware);
        return new(3,system,hw,Regex.IsMatch(description,"vmware|virtualbox|virtual machine|kvm|qemu|xen|parallels",RegexOptions.IgnoreCase),platform);
    }
    public static string DeviceId(string product,DeviceProfile p)=>Hash("LicenovaDevice/v3\n"+product.ToLowerInvariant()+"\n"+p.SystemId+"\n"+p.HardwareId);
    public static string DeviceId(string product)=>DeviceId(product,Profile(product));
}

public sealed record DeviceProfile(int Version,string SystemId,string HardwareId,bool VirtualMachine,string Platform);
