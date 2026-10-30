#ifndef LICENTIVO_DEVICE_DISPLAY_H
#define LICENTIVO_DEVICE_DISPLAY_H

#if defined(_WIN32) && !defined(PROCESSOR_ARCHITECTURE_ARM64)
#define PROCESSOR_ARCHITECTURE_ARM64 12
#endif

#ifndef _WIN32
#include <sys/statvfs.h>
#ifdef __APPLE__
#include <sys/sysctl.h>
#endif
#endif

/* Optional, cached display information. Never part of the licensing identity. */
#ifdef _WIN32
static void display_registry(const wchar_t*path,const wchar_t*value,char*out,size_t size){
 HKEY key;out[0]=0;if(RegOpenKeyExW(HKEY_LOCAL_MACHINE,path,0,KEY_READ|KEY_WOW64_64KEY,&key)!=ERROR_SUCCESS)return;
 wchar_t text_value[512]={0};DWORD bytes=sizeof(text_value),type_value=0;
 if(RegQueryValueExW(key,value,NULL,&type_value,(BYTE*)text_value,&bytes)==ERROR_SUCCESS&&(type_value==REG_SZ||type_value==REG_EXPAND_SZ)){text_value[511]=0;WideCharToMultiByte(CP_UTF8,0,text_value,-1,out,(int)size,NULL,NULL);}RegCloseKey(key);
}
#endif
static void display_text(cJSON*info,const char*key,const char*value,size_t limit){
 char clean[1024];size_t used=0,count=0;
 for(size_t i=0;value[i]&&count<limit&&used+4<sizeof(clean);){
  unsigned char ch=(unsigned char)value[i];size_t n=ch<0x80?1:ch>=0xc2&&ch<=0xdf?2:ch>=0xe0&&ch<=0xef?3:ch>=0xf0&&ch<=0xf4?4:0;
  if(!n){i++;continue;}int valid=1;for(size_t k=1;k<n;k++)if(!value[i+k]||((unsigned char)value[i+k]&0xc0)!=0x80)valid=0;if(!valid){i++;continue;}
  if(!(n==1&&(ch<32||ch==127))&&!(n==2&&ch==0xc2&&(unsigned char)value[i+1]<0xa0)){memcpy(clean+used,value+i,n);used+=n;count++;}i+=n;
 }
 while(used&&isspace((unsigned char)clean[used-1]))used--;
 clean[used]=0;
 cJSON_AddStringToObject(info,key,clean);
}
static void collect_display_details(cJSON*cfg){
 char system[512]={0},cpu[512]={0},gpu[1024]={0},arch[40]={0};uint64_t memory=0,storage=0;
#ifdef _WIN32
 const wchar_t*version=L"SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion";
 char product[256],build[64],edition[64];display_registry(version,L"ProductName",product,sizeof(product));display_registry(version,L"CurrentBuildNumber",build,sizeof(build));display_registry(version,L"DisplayVersion",edition,sizeof(edition));
 if(atoi(build)>=22000){char*ten=strstr(product,"Windows 10");if(ten)ten[9]='1';}
 snprintf(system,sizeof(system),"%s %s (%s)",product,edition,build);
 display_registry(L"HARDWARE\\DESCRIPTION\\System\\CentralProcessor\\0",L"ProcessorNameString",cpu,sizeof(cpu));
 ULONGLONG kb=0;typedef BOOL(WINAPI*display_memory_fn)(PULONGLONG);
 display_memory_fn read_memory=(display_memory_fn)GetProcAddress(GetModuleHandleW(L"kernel32.dll"),"GetPhysicallyInstalledSystemMemory");if(read_memory&&read_memory(&kb))memory=kb/1024;
 SYSTEM_INFO native;GetNativeSystemInfo(&native);snprintf(arch,sizeof(arch),"%s",native.wProcessorArchitecture==PROCESSOR_ARCHITECTURE_AMD64?"x64":native.wProcessorArchitecture==PROCESSOR_ARCHITECTURE_ARM64?"arm64":"x86");
 wchar_t drive[MAX_PATH];UINT n=GetSystemDirectoryW(drive,MAX_PATH);if(n&&n<MAX_PATH){drive[3]=0;ULARGE_INTEGER free_value,total,available;if(GetDiskFreeSpaceExW(drive,&available,&total,&free_value))storage=total.QuadPart/1073741824;}
 HKEY video;if(RegOpenKeyExW(HKEY_LOCAL_MACHINE,L"SYSTEM\\CurrentControlSet\\Control\\Video",0,KEY_READ,&video)==ERROR_SUCCESS){
  for(DWORD i=0;i<32;i++){wchar_t name[128];DWORD count=128;if(RegEnumKeyExW(video,i,name,&count,NULL,NULL,NULL,NULL)!=ERROR_SUCCESS)break;wchar_t path[256];swprintf(path,256,L"SYSTEM\\CurrentControlSet\\Control\\Video\\%ls\\0000",name);char model[256];display_registry(path,L"DriverDesc",model,sizeof(model));if(model[0]&&!strstr(gpu,model)&&strlen(gpu)+strlen(model)+4<sizeof(gpu)){if(gpu[0])strcat(gpu," / ");strcat(gpu,model);}}
  RegCloseKey(video);
 }
#else
 struct statvfs disk;if(statvfs("/",&disk)==0)storage=(uint64_t)disk.f_blocks*disk.f_frsize/1073741824;
#if defined(__aarch64__) || defined(__arm64__)
 strcpy(arch,"arm64");
#elif defined(__x86_64__)
 strcpy(arch,"x64");
#elif defined(__arm__)
 strcpy(arch,"arm");
#else
 strcpy(arch,"x86");
#endif
#ifdef __APPLE__
 size_t size=sizeof(cpu);if(sysctlbyname("machdep.cpu.brand_string",cpu,&size,NULL,0)!=0)cpu[0]=0;
 uint64_t bytes=0;size=sizeof(bytes);if(sysctlbyname("hw.memsize",&bytes,&size,NULL,0)==0)memory=bytes/1048576;
 char version[128]={0};size=sizeof(version);if(sysctlbyname("kern.osproductversion",version,&size,NULL,0)==0)snprintf(system,sizeof(system),"macOS %s",version);
#else
 FILE*file=fopen("/etc/os-release","r");char line[4096];if(file){while(fgets(line,sizeof(line),file)){if(!strncmp(line,"PRETTY_NAME=",12)){char*value=line+12;while(*value=='"'||isspace((unsigned char)*value))value++;size_t length=strcspn(value,"\r\n");while(length&&(value[length-1]=='"'||isspace((unsigned char)value[length-1])))length--;snprintf(system,sizeof(system),"%.*s",(int)length,value);break;}}fclose(file);}
 char implementer[64]={0},part[64]={0};file=fopen("/proc/cpuinfo","r");if(file){while(fgets(line,sizeof(line),file)){char*value=strchr(line,':');if(!value)continue;value++;while(isspace((unsigned char)*value))value++;if(!strncmp(line,"model name",10)||!strncmp(line,"Hardware",8)||!strncmp(line,"Processor",9)){snprintf(cpu,sizeof(cpu),"%s",value);break;}if(!implementer[0]&&!strncmp(line,"CPU implementer",15))snprintf(implementer,sizeof(implementer),"%s",value);if(!part[0]&&!strncmp(line,"CPU part",8))snprintf(part,sizeof(part),"%s",value);}fclose(file);}if(!cpu[0]&&part[0])snprintf(cpu,sizeof(cpu),"ARM CPU (implementer %s, part %s)",implementer,part);
 long pages=sysconf(_SC_PHYS_PAGES),page_size=sysconf(_SC_PAGESIZE);if(pages>0&&page_size>0)memory=(uint64_t)pages*(uint64_t)page_size/1048576;
#endif
#endif
 cJSON*info=cJSON_CreateObject();display_text(info,"os_version",system,160);display_text(info,"cpu",cpu,160);display_text(info,"gpu",gpu,240);display_text(info,"system_type",arch,40);cJSON_AddNumberToObject(info,"memory_mb",(double)memory);cJSON_AddNumberToObject(info,"storage_gb",(double)storage);
 cJSON_DeleteItemFromObjectCaseSensitive(cfg,"device_info");cJSON_AddItemToObject(cfg,"device_info",info);
}
#endif
