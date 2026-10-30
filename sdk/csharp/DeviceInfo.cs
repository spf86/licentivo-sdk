using System.Text;
using System.Diagnostics;
using System.Runtime.InteropServices;
using Microsoft.Win32;

namespace Licentivo;

public sealed record DeviceInfo(string Name,string Code,string Platform,string OSVersion="",string CPU="",long MemoryMB=0,string GPU="",string SystemType="",long StorageGB=0);

public sealed partial class LicentivoClient {
    private sealed record DisplayDetails(string OSVersion="",string CPU="",long MemoryMB=0,string GPU="",string SystemType="",long StorageGB=0);
    private readonly Lazy<DisplayDetails> displayDetails=new(ReadDisplayDetails);
    [DllImport("kernel32.dll")] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool GetPhysicallyInstalledSystemMemory(out ulong memory);
    private static string CleanDisplay(string value,int max=160)=>string.Concat(value.EnumerateRunes().Where(r=>!Rune.IsControl(r)).Take(max)).Trim();
    private static DisplayDetails ReadDisplayDetails(){
        string system="",cpu="",gpu="";long memory=0,storage=0;
        string Run(string file,params string[] args){try{var start=new ProcessStartInfo(file){UseShellExecute=false,RedirectStandardOutput=true,RedirectStandardError=true,CreateNoWindow=true};foreach(var arg in args)start.ArgumentList.Add(arg);using var process=Process.Start(start);if(process is null)return "";if(!process.WaitForExit(2000)){process.Kill();return "";}return process.StandardOutput.ReadToEnd().Trim();}catch{return "";}}
        try{
            if(OperatingSystem.IsWindows()){
                const string key=@"HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion";
                system=Registry.GetValue(key,"ProductName","")?.ToString()??"";var build=Registry.GetValue(key,"CurrentBuildNumber","")?.ToString()??"";
                if(int.TryParse(build,out int number)&&number>=22000)system=system.Replace("Windows 10","Windows 11");
                system+=" "+Registry.GetValue(key,"DisplayVersion","")+" ("+build+")";
                cpu=Registry.GetValue(@"HKEY_LOCAL_MACHINE\HARDWARE\DESCRIPTION\System\CentralProcessor\0","ProcessorNameString","")?.ToString()??"";
                if(GetPhysicallyInstalledSystemMemory(out ulong kb))memory=(long)(kb/1024);
                using var video=Registry.LocalMachine.OpenSubKey(@"SYSTEM\CurrentControlSet\Control\Video");var cards=new HashSet<string>();
                if(video is not null)foreach(var name in video.GetSubKeyNames().Take(32)){using var card=video.OpenSubKey(name+@"\0000");var value=card?.GetValue("DriverDesc")?.ToString();if(!string.IsNullOrWhiteSpace(value))cards.Add(value);}gpu=string.Join(" / ",cards);
            }else if(OperatingSystem.IsMacOS()){
                system=Run("/usr/bin/sw_vers","-productName")+" "+Run("/usr/bin/sw_vers","-productVersion");cpu=Run("/usr/sbin/sysctl","-n","machdep.cpu.brand_string");if(long.TryParse(Run("/usr/sbin/sysctl","-n","hw.memsize"),out var bytes))memory=bytes/1048576;
            }else{
                system=File.ReadLines("/etc/os-release").FirstOrDefault(line=>line.StartsWith("PRETTY_NAME="))?[12..].Trim('"')??"";
                var values=new Dictionary<string,string>();foreach(var line in File.ReadLines("/proc/cpuinfo")){var parts=line.Split(':',2);if(parts.Length==2)values.TryAdd(parts[0].Trim(),parts[1].Trim());}
                cpu=new[]{"model name","Hardware","Processor"}.Select(key=>values.GetValueOrDefault(key,"")).FirstOrDefault(value=>value.Length>0)??"";
                if(cpu.Length==0&&values.TryGetValue("CPU part",out var part))cpu="ARM CPU (implementer "+values.GetValueOrDefault("CPU implementer","")+", part "+part+")";
                var raw=File.ReadLines("/proc/meminfo").FirstOrDefault(line=>line.StartsWith("MemTotal:"))?.Split((char[]?)null,StringSplitOptions.RemoveEmptyEntries);if(raw?.Length>1&&long.TryParse(raw[1],out var kb))memory=kb/1024;
            }
        }catch{}
        try{storage=new DriveInfo(Path.GetPathRoot(Environment.SystemDirectory.Length>0?Environment.SystemDirectory:Environment.CurrentDirectory)??"/").TotalSize/1073741824;}catch{}
        return new(CleanDisplay(system),CleanDisplay(cpu),memory,CleanDisplay(gpu,240),RuntimeInformation.OSArchitecture.ToString().ToLowerInvariant(),storage);
    }
    private static string DefaultDeviceName(){
        string name="";try{name=Environment.MachineName;}catch{}
        name=string.Concat(name.EnumerateRunes().Where(r=>!System.Text.Rune.IsControl(r)).Take(120)).Trim();
        return name.Length>0?name:OperatingSystem.IsWindows()?"Windows":OperatingSystem.IsMacOS()?"macOS":"Linux";
    }
    public DeviceInfo GetDeviceInfo(){var details=displayDetails.Value;return new(config.DeviceName,(config.DeviceId[..4]+"-"+config.DeviceId[^4..]).ToUpperInvariant(),profile.Platform,details.OSVersion,details.CPU,details.MemoryMB,details.GPU,details.SystemType,details.StorageGB);}
}
