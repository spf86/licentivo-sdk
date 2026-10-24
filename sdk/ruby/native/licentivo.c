#define _POSIX_C_SOURCE 200809L
#ifdef _WIN32
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0600
#endif
#endif
#include "licentivo.h"
#include <curl/curl.h>
#include "vendor/monocypher-ed25519.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <time.h>
#include <math.h>
#include <errno.h>
#include <sys/stat.h>
#include <wchar.h>
#ifdef _WIN32
#include <windows.h>
#include <bcrypt.h>
#include <direct.h>
#include <io.h>
/* State journals can exceed MAX_PATH under a caller's cache directory. Use
 * absolute UTF-16 paths with the extended prefix for every state-file operation. */
static wchar_t*state_windows_path(const char*path){
 int size=MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,path,-1,NULL,0);if(!size){errno=EINVAL;return NULL;}
 wchar_t*input=malloc((size_t)size*sizeof(wchar_t));if(!input){errno=ENOMEM;return NULL;}
 if(!MultiByteToWideChar(CP_UTF8,MB_ERR_INVALID_CHARS,path,-1,input,size)){free(input);errno=EINVAL;return NULL;}
 for(wchar_t*p=input;*p;p++)if(*p==L'/')*p=L'\\';
 if(!wcsncmp(input,L"\\\\?\\",4))return input;
 DWORD count=GetFullPathNameW(input,0,NULL,NULL);if(!count||count>32760){free(input);errno=EINVAL;return NULL;}
 wchar_t*absolute=malloc((size_t)count*sizeof(wchar_t));if(!absolute){free(input);errno=ENOMEM;return NULL;}
 DWORD written=GetFullPathNameW(input,count,absolute,NULL);free(input);if(!written||written>=count){free(absolute);errno=EINVAL;return NULL;}
 int unc=absolute[0]==L'\\'&&absolute[1]==L'\\';
 wchar_t*result=malloc(((size_t)written+9)*sizeof(wchar_t));if(!result){free(absolute);errno=ENOMEM;return NULL;}
 wcscpy(result,unc?L"\\\\?\\UNC\\":L"\\\\?\\");wcscat(result,absolute+(unc?2:0));free(absolute);return result;
}
static FILE*state_windows_open(const char*path,const char*mode){
 wchar_t*p=state_windows_path(path),wide_mode[8];if(!p)return NULL;size_t length=strlen(mode);if(length>=8){free(p);errno=EINVAL;return NULL;}
 for(size_t i=0;i<=length;i++){wide_mode[i]=(wchar_t)(unsigned char)mode[i];}
 FILE*f=_wfopen(p,wide_mode);free(p);return f;
}
static int state_windows_remove(const char*path){wchar_t*p=state_windows_path(path);if(!p)return -1;int result=_wremove(p);free(p);return result;}
static int state_windows_mkdir(const char*path){wchar_t*p=state_windows_path(path);if(!p)return -1;int result=_wmkdir(p);free(p);return result;}
static int state_windows_stat(const char*path,struct stat*info){
 wchar_t*p=state_windows_path(path);if(!p)return -1;WIN32_FILE_ATTRIBUTE_DATA data;BOOL ok=GetFileAttributesExW(p,GetFileExInfoStandard,&data);DWORD error=GetLastError();free(p);
 if(!ok){errno=error==ERROR_FILE_NOT_FOUND||error==ERROR_PATH_NOT_FOUND?ENOENT:EACCES;return -1;}
 memset(info,0,sizeof(*info));info->st_size=data.nFileSizeHigh||data.nFileSizeLow>2147483647?2147483647:(long)data.nFileSizeLow;return 0;
}
static int state_windows_move(const char*from,const char*to){wchar_t*a=state_windows_path(from),*b=state_windows_path(to);int ok=a&&b&&MoveFileExW(a,b,MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH);free(a);free(b);return ok;}
#define fopen(path,mode) state_windows_open(path,mode)
#define remove(path) state_windows_remove(path)
#define stat(path,info) state_windows_stat(path,info)
#define mkdir_private(p) state_windows_mkdir(p)
#else
#include <openssl/evp.h>
#include <openssl/rand.h>
#include <pthread.h>
#include <stdatomic.h>
#include <unistd.h>
#include <dirent.h>
#define mkdir_private(p) mkdir(p,0700)
#endif
#define LIMIT 16384

/* Initialize once before concurrent clients; cleanup belongs to the host process. */
#ifdef _WIN32
static INIT_ONCE curl_once = INIT_ONCE_STATIC_INIT;
static int curl_ready;
static BOOL CALLBACK initialize_curl(PINIT_ONCE once, PVOID parameter, PVOID *context) {
 (void)once; (void)parameter; (void)context;
 curl_ready = curl_global_init(CURL_GLOBAL_DEFAULT) == CURLE_OK;
 return TRUE;
}
static int ensure_curl(void) { InitOnceExecuteOnce(&curl_once, initialize_curl, NULL, NULL); return curl_ready; }
#else
static pthread_once_t curl_once = PTHREAD_ONCE_INIT;
static int curl_ready;
static void initialize_curl(void) { curl_ready = curl_global_init(CURL_GLOBAL_DEFAULT) == CURLE_OK; }
static int ensure_curl(void) { return pthread_once(&curl_once, initialize_curl) == 0 && curl_ready; }
#endif

struct ln_client {
 cJSON *cfg,*cached,*auth,*logout_state,*logout_bindings;int auth_dirty;char session[37];int interval;int offline;int64_t wall;double tick;char error[80];int started;int finished;int refresh_pending;int online;int retryable;long http_status;int retry_after;char request_id[128];int64_t retry_at;
 cJSON *view;int64_t view_wall;double view_tick;int view_online;ln_error_info view_error;ln_status_callback callback;void *callback_data;int task_busy;
#ifdef _WIN32
 CRITICAL_SECTION mutex,view_mutex;HANDLE thread;volatile LONG stop,denied;
#else
 pthread_mutex_t mutex,view_mutex;pthread_t thread;atomic_int stop,denied;
#endif
};
static int is_denied(ln_client*c){
#ifdef _WIN32
 return InterlockedCompareExchange(&c->denied,0,0)!=0;
#else
 return atomic_load(&c->denied);
#endif
}
static void set_denied(ln_client*c){
#ifdef _WIN32
 InterlockedExchange(&c->denied,1);
#else
 atomic_store(&c->denied,1);
#endif
}
static void load_logout_state(ln_client*c);static int deny_runtime(ln_client*c);static int revocation_error(const char*code);static int retry_logouts_unlocked(ln_client*c);static int has_pending_logouts(ln_client*c);
static const cJSON *get(const cJSON *o,const char*k){return cJSON_GetObjectItemCaseSensitive(o,k);}
static const char *text(const cJSON *o,const char*k){const cJSON*v=get(o,k);return cJSON_IsString(v)?v->valuestring:NULL;}
static int num(const cJSON*o,const char*k,int64_t*out){const cJSON*v=get(o,k);if(!cJSON_IsNumber(v)||!isfinite(v->valuedouble)||v->valuedouble!=floor(v->valuedouble)||fabs(v->valuedouble)>9007199254740991.0)return 0;*out=(int64_t)v->valuedouble;return 1;}
static int unique(const cJSON*v,int depth){if(depth>32)return 0;if(cJSON_IsObject(v)){for(const cJSON*a=v->child;a;a=a->next){for(const cJSON*b=a->next;b;b=b->next)if(!strcmp(a->string,b->string))return 0;if(!unique(a,depth+1))return 0;}}else if(cJSON_IsArray(v))for(const cJSON*a=v->child;a;a=a->next)if(!unique(a,depth+1))return 0;return 1;}
cJSON *ln_json(const char*s){const char*end=NULL;cJSON*v=cJSON_ParseWithOpts(s,&end,1);if(!v||!unique(v,0)){cJSON_Delete(v);return NULL;}return v;}
static int fields(const cJSON*v,const char*const*names,int n){if(!cJSON_IsObject(v)||cJSON_GetArraySize(v)!=n)return 0;for(int i=0;i<n;i++)if(!get(v,names[i]))return 0;return 1;}
static int uuid(const char*s){if(!s||strlen(s)!=36)return 0;for(int i=0;i<36;i++){if(i==8||i==13||i==18||i==23){if(s[i]!='-')return 0;}else if(!isxdigit((unsigned char)s[i]))return 0;}return 1;}
static int device(const char*s){if(!s||strlen(s)!=64)return 0;for(int i=0;i<64;i++)if(!((s[i]>='a'&&s[i]<='f')||(s[i]>='0'&&s[i]<='9')))return 0;return 1;}
static int credential_secret(const char*s,const char*prefix){if(!s||strlen(s)!=50||strncmp(s,prefix,7))return 0;for(int i=7;i<50;i++)if(!isalnum((unsigned char)s[i])&&s[i]!='_'&&s[i]!='-')return 0;return 1;}
static const char*source_input(ln_client*c){const char*identity=text(c->cfg,"identity_token");return identity&&identity[0]?identity:text(c->cfg,"license_key");}
static int source_hash(ln_client*c,char out[65]);
static int temporary(ln_client*c){return c->auth!=NULL||source_input(c)[0]!=0;}
static const char*runtime_key(ln_client*c){const char*k=text(c->auth,"device_key");return k&&k[0]?k:text(c->cfg,"license_key");}
static void publish(ln_client*c,int online);
static void notify(ln_client*c);
static void load_credential(ln_client*c);
static int prepare_credential(ln_client*c);
static int accept_credential(ln_client*c,const cJSON*result);
static int save_logout_state(ln_client*c);
static int logout_binding_blocked(ln_client*c,const cJSON*result);
static const char b64chars[]="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
static int b64(const char*s,unsigned char*out,size_t capacity){if(!s||!s[0])return -1;unsigned int acc=0,bits=0;size_t used=0;for(size_t i=0;s[i];i++){const char*p=strchr(b64chars,s[i]);if(!p||s[i]=='=')return -1;acc=(acc<<6)|(unsigned int)(p-b64chars);bits+=6;if(bits>=8){bits-=8;if(used>=capacity)return -1;out[used++]=(unsigned char)(acc>>bits);}}if(bits==6||(bits&&(acc&((1u<<bits)-1u))))return -1;return (int)used;}
static int64_t policy_span(const cJSON*c,int64_t issued){
 const cJSON*p=get(c,"runtime_policy");if(!p)return 86400;
 static const char*const names[]={"offline_mode","heartbeat_seconds","offline_seconds"};int64_t h,o;const char*m=text(p,"offline_mode");
 if(!fields(p,names,3)||!m||!num(p,"heartbeat_seconds",&h)||!num(p,"offline_seconds",&o))return 0;
 if(h!=0&&(h<10||h>86400))return 0;
 if(!strcmp(m,"permanent")&&o==0)return 253402300799LL-issued;
 if(!strcmp(m,"none")&&o==0)return h==0?10:h+(h+9)/10+30;
 if(!strcmp(m,"limited")&&o>=1&&o<=31536000)return o;
 return 0;
}
/* Called only for an envelope whose signature and context have already been verified. */
static int envelope_policy(const cJSON*e,int*heartbeat){
 unsigned char raw[LIMIT+1];int n=b64(text(e,"payload"),raw,LIMIT);if(n<0)return 0;raw[n]=0;cJSON*c=ln_json((char*)raw);const cJSON*p=get(c,"runtime_policy");const char*m=text(p,"offline_mode");int64_t h;int mode=0;
 if(m){if(!strcmp(m,"none"))mode=1;else if(!strcmp(m,"permanent"))mode=2;if(heartbeat&&num(p,"heartbeat_seconds",&h))*heartbeat=(int)h;}
 cJSON_Delete(c);return mode;
}
static int claim_fields(const cJSON*c){static const char*const allowed[]={"version","key_id","tenant_id","product_id","license_id","device_id","activation_id","mode","features","max_devices","issued_at","expires_at","runtime_policy","capabilities","offline_request_id"};if(!cJSON_IsObject(c))return 0;for(const cJSON*v=c->child;v;v=v->next){int ok=0;for(int i=0;i<15;i++)if(!strcmp(v->string,allowed[i]))ok=1;if(!ok)return 0;}return cJSON_GetArraySize(c)>=12;}
static int app_version(const char*s){if(!s)return 0;const char*p=s;for(int i=0;i<3;i++){int n=0;if(*p<'0'||*p>'9')return 0;if(*p=='0'&&p[1]>='0'&&p[1]<='9')return 0;while(*p>='0'&&*p<='9'){p++;if(++n>9)return 0;}if(i<2){if(*p++!='.')return 0;}else if(*p)return 0;}return 1;}
static int advanced_claims(const cJSON*c){
 const cJSON*request=get(c,"offline_request_id");if(request&&!uuid(text(c,"offline_request_id")))return 0;const cJSON*x=get(c,"capabilities");if(!x)return 1;if(!cJSON_IsObject(x))return 0;
 const char*model=text(x,"license_model");if(!model||(strcmp(model,"node_locked")&&strcmp(model,"floating")))return 0;
 const char*const allowed[]={"license_model","session_id","session_expires_at","app_version","version_released_at","maintenance_until","launch_connection_id","environment","binding_version"};for(const cJSON*v=x->child;v;v=v->next){int ok=0;for(int i=0;i<9;i++)if(!strcmp(v->string,allowed[i]))ok=1;if(!ok)return 0;}
 int64_t session=0,released=0,maintenance=0,issued=0,expires=0;const char*const dates[]={"session_expires_at","version_released_at","maintenance_until"};int64_t*values[]={&session,&released,&maintenance};for(int i=0;i<3;i++)if(get(x,dates[i])&&(!num(x,dates[i],values[i])||*values[i]<0||*values[i]>253402300799LL))return 0;
 if(get(x,"app_version")&&!app_version(text(x,"app_version")))return 0;
 if(!num(c,"issued_at",&issued)||!num(c,"expires_at",&expires))return 0;
 const cJSON*policy=get(c,"runtime_policy");int64_t heartbeat=0;if(!strcmp(model,"floating")&&(!uuid(text(x,"session_id"))||session<=issued||expires>session||!num(policy,"heartbeat_seconds",&heartbeat)||heartbeat==0||!strcmp(text(policy,"offline_mode")?text(policy,"offline_mode"):"","permanent")))return 0;
 if(get(x,"launch_connection_id")||get(x,"environment")||get(x,"binding_version")){int64_t binding=0,h=0,o=0;const char*env=text(x,"environment"),*mode=text(policy,"offline_mode");if(!uuid(text(x,"launch_connection_id"))||!env||(strcmp(env,"live")&&strcmp(env,"sandbox"))||!num(x,"binding_version",&binding)||binding<1||strcmp(model,"node_locked")||!uuid(text(x,"session_id"))||session<=issued||expires>session||!num(policy,"heartbeat_seconds",&h)||h<600||!mode||!strcmp(mode,"permanent")||!num(policy,"offline_seconds",&o)||o>86400)return 0;}
 if(released>issued||(maintenance>0&&(!text(x,"app_version")||released==0||released>maintenance)))return 0;
 return 1;
}
int ln_verify(const cJSON*e,const cJSON*keys,const char*product,const char*dev,int64_t now){
 static const char*const envfields[]={"version","key_id","payload","signature"};
 int64_t version,max,issued,expires;const char*id=text(e,"key_id");if(!fields(e,envfields,4)||!num(e,"version",&version)||version!=1||!id)return 0;const char*pin=text(keys,id);unsigned char pub[32],sig[64],raw[LIMIT+1];int n=b64(text(e,"payload"),raw,LIMIT);if(n<0||b64(pin,pub,32)!=32||b64(text(e,"signature"),sig,64)!=64||memchr(raw,0,(size_t)n))return 0;
 unsigned char message[LIMIT+32];const char*prefix="LicenovaLease/v1\n";size_t plen=strlen(prefix);memcpy(message,prefix,plen);memcpy(message+plen,raw,(size_t)n);
 int valid=crypto_ed25519_check(sig,pub,message,plen+(size_t)n)==0;if(!valid)return 0;raw[n]=0;cJSON*c=ln_json((char*)raw);if(!c)return 0;
 const char*mode=text(c,"mode");valid=claim_fields(c)&&advanced_claims(c)&&num(c,"version",&version)&&version==1&&text(c,"key_id")&&!strcmp(text(c,"key_id"),id)&&text(c,"product_id")&&product&&!strcmp(text(c,"product_id"),product)&&text(c,"device_id")&&dev&&!strcmp(text(c,"device_id"),dev)&&device(dev)&&uuid(text(c,"tenant_id"))&&uuid(product)&&uuid(text(c,"license_id"))&&uuid(text(c,"activation_id"))&&mode&&(!strcmp(mode,"perpetual")||!strcmp(mode,"timed")||!strcmp(mode,"trial"))&&num(c,"max_devices",&max)&&max>=1&&max<=10000&&num(c,"issued_at",&issued)&&num(c,"expires_at",&expires)&&issued>0&&expires>issued&&expires<=253402300799LL&&expires-issued<=policy_span(c,issued)&&issued<=now+300&&now<expires;
 const cJSON*features=get(c,"features");if(!cJSON_IsArray(features)||cJSON_GetArraySize(features)>32)valid=0;const char*previous="";for(const cJSON*f=features?features->child:NULL;f;f=f->next){if(!cJSON_IsString(f)||!f->valuestring[0]||strlen(f->valuestring)>64){valid=0;break;}const char*s=f->valuestring;for(int i=0;s[i];i++)if(!((s[i]>='a'&&s[i]<='z')||(s[i]>='A'&&s[i]<='Z')||(s[i]>='0'&&s[i]<='9')||s[i]=='_'||s[i]=='-'||s[i]=='.'))valid=0;if(strcmp(previous,s)>=0)valid=0;previous=s;}cJSON_Delete(c);return valid;
}
static int hash(const char*s,char out[65]){unsigned char raw[32]={0};int ok=0;
#ifdef _WIN32
 BCRYPT_ALG_HANDLE alg=NULL;BCRYPT_HASH_HANDLE digest=NULL;if(BCryptOpenAlgorithmProvider(&alg,BCRYPT_SHA256_ALGORITHM,NULL,0)>=0&&BCryptCreateHash(alg,&digest,NULL,0,NULL,0,0)>=0){ok=BCryptHashData(digest,(PUCHAR)s,(ULONG)strlen(s),0)>=0&&BCryptFinishHash(digest,raw,32,0)>=0;}if(digest)BCryptDestroyHash(digest);if(alg)BCryptCloseAlgorithmProvider(alg,0);
#else
 unsigned int n=0;ok=EVP_Digest(s,strlen(s),raw,&n,EVP_sha256(),NULL)==1&&n==32;
#endif
 if (!ok) return 0;
 for (int i=0; i<32; i++) { sprintf(out+i*2,"%02x",raw[i]); }
 out[64]=0;
 return 1;
}
/* OS identity is read locally; app folders and cache files cannot provide it. */
static int system_device_id(const char*product,char out[65]){
 if(!uuid(product))return 0;
 char identity[128]={0};
#ifdef _WIN32
 HKEY key=NULL;DWORD size=sizeof(identity),type=0;
 if(RegOpenKeyExA(HKEY_LOCAL_MACHINE,"SOFTWARE\\Microsoft\\Cryptography",0,KEY_READ|KEY_WOW64_64KEY,&key)!=ERROR_SUCCESS)return 0;
 LONG result=RegQueryValueExA(key,"MachineGuid",NULL,&type,(LPBYTE)identity,&size);RegCloseKey(key);
 if(result!=ERROR_SUCCESS||type!=REG_SZ||size>=sizeof(identity))return 0;
#elif defined(__APPLE__)
 FILE*f=popen("/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice","r");if(!f)return 0;char line[2048];
 while(fgets(line,sizeof(line),f)){char*p=strstr(line,"\"IOPlatformUUID\"");if(!p)continue;p=strchr(p,'=');if(!p)continue;p=strchr(p,'"');if(!p)continue;p++;char*end=strchr(p,'"');if(end&&end-p<127){memcpy(identity,p,(size_t)(end-p));break;}}
 pclose(f);
#else
 const char*paths[]={"/etc/machine-id","/var/lib/dbus/machine-id"};for(int i=0;i<2;i++){FILE*f=fopen(paths[i],"rb");if(!f)continue;size_t n=fread(identity,1,127,f);fclose(f);identity[n]=0;if(n)break;}
#endif
 char*start=identity;while(isspace((unsigned char)*start))start++;size_t len=strlen(start);while(len&&isspace((unsigned char)start[len-1]))start[--len]=0;
 for(size_t i=0;i<len;i++)start[i]=(char)tolower((unsigned char)start[i]);
 int valid=(len==36&&uuid(start))||len==32,nonzero=0;for(size_t i=0;i<len;i++){if(start[i]!='-'&&!isxdigit((unsigned char)start[i]))valid=0;if(start[i]!='-'&&start[i]!='0')nonzero=1;}if(!valid||!nonzero)return 0;
 char normalized[37];for(int i=0;i<36;i++)normalized[i]=(char)tolower((unsigned char)product[i]);normalized[36]=0;
 char context[256];snprintf(context,sizeof(context),"LicenovaDevice/v2\n%s\n%s",normalized,start);return hash(context,out);
}
static int random_bytes(unsigned char*out,int n){
#ifdef _WIN32
 return BCryptGenRandom(NULL,out,(ULONG)n,BCRYPT_USE_SYSTEM_PREFERRED_RNG)>=0;
#else
 return RAND_bytes(out,n)==1;
#endif
}
static int request_uuid(char out[37]){unsigned char b[16];if(!random_bytes(b,16))return 0;b[6]=(b[6]&15)|64;b[8]=(b[8]&63)|128;snprintf(out,37,"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",b[0],b[1],b[2],b[3],b[4],b[5],b[6],b[7],b[8],b[9],b[10],b[11],b[12],b[13],b[14],b[15]);return 1;}
static cJSON*machine_profile(const char*product,char dev[65]){
 char system[65],hardware[128]={0},description[2048]={0},hw[65]={0},context[512];if(!system_device_id(product,system))return NULL;const char*platform;
#ifdef _WIN32
 platform="windows";HANDLE reader=NULL,writer=NULL;SECURITY_ATTRIBUTES security={sizeof(security),NULL,TRUE};
 if(CreatePipe(&reader,&writer,&security,0)){SetHandleInformation(reader,HANDLE_FLAG_INHERIT,0);STARTUPINFOA startup={0};PROCESS_INFORMATION process={0};startup.cb=sizeof(startup);startup.dwFlags=STARTF_USESTDHANDLES;startup.hStdOutput=writer;startup.hStdError=writer;
 char command[]="powershell.exe -NoProfile -NonInteractive -Command \"(Get-CimInstance Win32_ComputerSystemProduct).UUID; $v=Get-CimInstance Win32_ComputerSystem; $v.Manufacturer; $v.Model\"";
 if(CreateProcessA(NULL,command,NULL,NULL,TRUE,CREATE_NO_WINDOW,NULL,NULL,&startup,&process)){CloseHandle(writer);writer=NULL;if(WaitForSingleObject(process.hProcess,5000)==WAIT_TIMEOUT)TerminateProcess(process.hProcess,1);DWORD n=0;ReadFile(reader,description,sizeof(description)-1,&n,NULL);description[n]=0;CloseHandle(process.hProcess);CloseHandle(process.hThread);const char*end=strchr(description,'\n');size_t len=end?(size_t)(end-description):strlen(description);if(len<sizeof(hardware))memcpy(hardware,description,len);}if(writer)CloseHandle(writer);CloseHandle(reader);}
#elif defined(__APPLE__)
 platform="darwin";FILE*f=popen("/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice","r");if(f){char line[2048];while(fgets(line,sizeof(line),f)){if(strstr(line,"VMware")||strstr(line,"Parallels"))strcpy(description,"virtual machine");char*p=strstr(line,"\"IOPlatformUUID\"");if(!p)continue;p=strchr(p,'=');if(!p)continue;p=strchr(p,'"');if(!p)continue;p++;char*end=strchr(p,'"');if(end&&end-p<127)memcpy(hardware,p,(size_t)(end-p));}pclose(f);}
#else
 platform="linux";FILE*f=fopen("/sys/class/dmi/id/product_uuid","rb");if(f){size_t n=fread(hardware,1,sizeof(hardware)-1,f);hardware[n]=0;fclose(f);}const char*paths[]={"/sys/class/dmi/id/sys_vendor","/sys/class/dmi/id/product_name","/.dockerenv"};for(int i=0;i<3;i++){f=fopen(paths[i],"rb");if(f){if(i==2)strcat(description," virtual machine");else{size_t used=strlen(description),n=fread(description+used,1,512,f);description[used+n]=0;strcat(description," ");}fclose(f);}}
#endif
 char*start=hardware;while(isspace((unsigned char)*start))start++;size_t len=strlen(start);while(len&&isspace((unsigned char)start[len-1]))start[--len]=0;int usable=0;for(size_t i=0;i<len;i++){start[i]=(char)tolower((unsigned char)start[i]);if(start[i]!='-'&&start[i]!='0'&&start[i]!='f')usable=1;}
 if(uuid(start)&&usable){snprintf(context,sizeof(context),"LicenovaHardware/v1\n%s\n%s",product,start);if(!hash(context,hw))return NULL;}
 for(size_t i=0;description[i];i++){description[i]=(char)tolower((unsigned char)description[i]);}
 int vm=strstr(description,"vmware")||strstr(description,"virtualbox")||strstr(description,"virtual machine")||strstr(description,"kvm")||strstr(description,"qemu")||strstr(description,"xen")||strstr(description,"parallels");
 snprintf(context,sizeof(context),"LicenovaDevice/v3\n%s\n%s\n%s",product,system,hw);if(!hash(context,dev))return NULL;cJSON*p=cJSON_CreateObject();cJSON_AddNumberToObject(p,"version",3);cJSON_AddStringToObject(p,"system_id",system);cJSON_AddStringToObject(p,"hardware_id",hw);cJSON_AddBoolToObject(p,"virtual_machine",vm!=0);cJSON_AddStringToObject(p,"platform",platform);return p;
}
static double monotonic(void){
#ifdef _WIN32
 return GetTickCount64()/1000.0;
#else
 struct timespec ts;clock_gettime(CLOCK_MONOTONIC,&ts);return ts.tv_sec+ts.tv_nsec/1e9;
#endif
}
static void lock(ln_client*c){
#ifdef _WIN32
 EnterCriticalSection(&c->mutex);
#else
 pthread_mutex_lock(&c->mutex);
#endif
}
static void unlock(ln_client*c){
#ifdef _WIN32
 LeaveCriticalSection(&c->mutex);
#else
 pthread_mutex_unlock(&c->mutex);
#endif
}
static char*readfile(const char*path,size_t limit){FILE*f=fopen(path,"rb");if(!f)return NULL;char*b=malloc(limit+2);if(!b){fclose(f);return NULL;}size_t n=fread(b,1,limit+1,f);fclose(f);if(n>limit||memchr(b,0,n)){free(b);return NULL;}b[n]=0;return b;}
static int origin(const cJSON*cfg){CURLU*u=curl_url();if(!u)return 0;int valid=0;char*scheme=NULL,*host=NULL,*path=NULL,*extra=NULL;const char*base=text(cfg,"base_url");if(!base||curl_url_set(u,CURLUPART_URL,base,0))goto done;curl_url_get(u,CURLUPART_SCHEME,&scheme,0);curl_url_get(u,CURLUPART_HOST,&host,0);curl_url_get(u,CURLUPART_PATH,&path,0);if(!scheme||!host||!path||strcmp(path,"/"))goto done;for(int i=0;i<4;i++){CURLUPart parts[]={CURLUPART_USER,CURLUPART_PASSWORD,CURLUPART_QUERY,CURLUPART_FRAGMENT};if(!curl_url_get(u,parts[i],&extra,0)){curl_free(extra);extra=NULL;goto done;}}
 valid=!strcmp(scheme,"https")||(!strcmp(scheme,"http")&&cJSON_IsTrue(get(cfg,"allow_http"))&&(!strcmp(host,"localhost")||!strcmp(host,"127.0.0.1")||!strcmp(host,"[::1]")));
done:curl_free(scheme);curl_free(host);curl_free(path);curl_url_cleanup(u);return valid;}
static cJSON*claims_from(const cJSON*envelope){unsigned char raw[LIMIT+1];int n=b64(text(envelope,"payload"),raw,LIMIT);if(n<0)return NULL;raw[n]=0;return ln_json((char*)raw);}
static int client_context(ln_client*c,const cJSON*envelope){cJSON*claims=claims_from(envelope);const cJSON*x=get(claims,"capabilities");int ok=1;const char*connection=text(c->cfg,"launch_connection_id"),*environment=text(c->cfg,"environment");if(!x&&connection&&connection[0])ok=0;if(x){const char*signed_connection=text(x,"launch_connection_id"),*signed_environment=text(x,"environment");if(strcmp(signed_connection?signed_connection:"",connection?connection:"")||strcmp(signed_environment?signed_environment:"",environment?environment:""))ok=0;const char*version=text(x,"app_version"),*expected=text(c->cfg,"app_version"),*model=text(x,"license_model");if(strcmp(version?version:"",expected?expected:""))ok=0;if(model&&!strcmp(model,"floating")&&(!text(x,"session_id")||strcmp(text(x,"session_id"),c->session)))ok=0;}cJSON_Delete(claims);return ok;}
static int floating(ln_client*c){cJSON*claims=claims_from(c->cached);const char*model=text(get(claims,"capabilities"),"license_model");int result=model&&!strcmp(model,"floating");cJSON_Delete(claims);return result;}
static int source_hash(ln_client*c,char out[65]){const char*source=source_input(c);if(source&&source[0])return hash(source,out);const char*h=text(c->auth,"source_hash");if(!device(h))return 0;strcpy(out,h);return 1;}
static int cache_valid(ln_client*c){return !is_denied(c)&&c->cached&&client_context(c,c->cached)&&(!c->wall||time(NULL)>=c->wall+(monotonic()-c->tick)-300)&&ln_verify(c->cached,get(c->cfg,"trusted_keys"),text(c->cfg,"product_id"),text(c->cfg,"device_id"),time(NULL));}
static void view_lock(ln_client*c){
#ifdef _WIN32
 EnterCriticalSection(&c->view_mutex);
#else
 pthread_mutex_lock(&c->view_mutex);
#endif
}
static void view_unlock(ln_client*c){
#ifdef _WIN32
 LeaveCriticalSection(&c->view_mutex);
#else
 pthread_mutex_unlock(&c->view_mutex);
#endif
}
static void publish(ln_client*c,int online){view_lock(c);cJSON_Delete(c->view);c->view=cJSON_Duplicate(c->cached,1);c->view_wall=c->wall;c->view_tick=c->tick;c->view_online=online;snprintf(c->view_error.code,sizeof(c->view_error.code),"%s",c->error);snprintf(c->view_error.request_id,sizeof(c->view_error.request_id),"%s",c->request_id);c->view_error.http_status=(int)c->http_status;c->view_error.retryable=c->retryable;c->view_error.retry_after_seconds=c->retry_after;view_unlock(c);}
static cJSON*local_claims(ln_client*c){if(is_denied(c))return NULL;view_lock(c);cJSON*envelope=cJSON_Duplicate(c->view,1);int64_t wall=c->view_wall;double tick=c->view_tick;view_unlock(c);int valid=envelope&&client_context(c,envelope)&&(!wall||time(NULL)>=wall+monotonic()-tick-300)&&ln_verify(envelope,get(c->cfg,"trusted_keys"),text(c->cfg,"product_id"),text(c->cfg,"device_id"),time(NULL));cJSON*claims=valid?claims_from(envelope):NULL;cJSON_Delete(envelope);return claims;}
int ln_get_error_info(ln_client*c,ln_error_info*out){if(!c||!out)return 0;view_lock(c);*out=c->view_error;view_unlock(c);return 1;}
char*ln_status(ln_client*c){if(!c)return NULL;cJSON*claims=local_claims(c),*status=cJSON_CreateObject();view_lock(c);ln_error_info e=c->view_error;int online=c->view_online;view_unlock(c);const char*state=claims?(online?"active":"offline_valid"):"verification_required";if(!strcmp(e.code,"LICENSE_EXPIRED"))state="expired";if(!strcmp(e.code,"LICENSE_REVOKED")||!strcmp(e.code,"LICENSE_SUSPENDED"))state="revoked";if(!strcmp(e.code,"DEVICE_RELEASED")||!strcmp(e.code,"DEVICE_CREDENTIAL_REVOKED"))state="released";if(!strcmp(e.code,"FEATURE_QUOTA_EXCEEDED"))state="quota_exhausted";int64_t expires=0;num(claims,"expires_at",&expires);cJSON_AddStringToObject(status,"state",state);cJSON_AddBoolToObject(status,"allowed",claims!=NULL);cJSON_AddStringToObject(status,"code",e.code);cJSON_AddStringToObject(status,"request_id",e.request_id);cJSON_AddBoolToObject(status,"retryable",e.retryable);cJSON_AddNumberToObject(status,"lease_valid_until",(double)expires);char*out=cJSON_PrintUnformatted(status);cJSON_Delete(status);cJSON_Delete(claims);return out;}
char*ln_feature_status(ln_client*c,const char*feature){
 if(!c||!feature)return NULL;
 char*raw=ln_status(c);cJSON*status=raw?ln_json(raw):NULL;ln_free_string(raw);
 if(!status)return NULL;
 if(cJSON_IsTrue(get(status,"allowed"))){
  cJSON*claims=local_claims(c);int entitled=0;const cJSON*features=get(claims,"features");
  for(const cJSON*f=features?features->child:NULL;f;f=f->next)if(cJSON_IsString(f)&&!strcmp(f->valuestring,feature))entitled=1;
  if(!entitled){cJSON_ReplaceItemInObjectCaseSensitive(status,"allowed",cJSON_CreateBool(0));cJSON_ReplaceItemInObjectCaseSensitive(status,claims?"code":"state",cJSON_CreateString(claims?"FEATURE_NOT_ENTITLED":"verification_required"));}
  cJSON_Delete(claims);
 }
 char*out=cJSON_PrintUnformatted(status);cJSON_Delete(status);return out;
}
void ln_on_status_changed(ln_client*c,ln_status_callback callback,void*data){if(!c)return;view_lock(c);c->callback=callback;c->callback_data=data;view_unlock(c);}
static void notify(ln_client*c){view_lock(c);ln_status_callback callback=c->callback;void*data=c->callback_data;view_unlock(c);if(callback){char*status=ln_status(c);if(status){callback(status,data);free(status);}}}
ln_client*ln_init(const char*config){if(!ensure_curl())return NULL;cJSON*cfg=ln_json(config);if(!cfg)return NULL;if(!text(cfg,"license_key"))cJSON_AddStringToObject(cfg,"license_key","");const char*base=text(cfg,"base_url");if(!base||!base[0]){cJSON_DeleteItemFromObjectCaseSensitive(cfg,"base_url");cJSON_AddStringToObject(cfg,"base_url","https://www.licentivo.com");base=text(cfg,"base_url");}if(base&&strlen(base)>0&&base[strlen(base)-1]=='/'){char*copy=malloc(strlen(base));if(!copy){cJSON_Delete(cfg);return NULL;}memcpy(copy,base,strlen(base)-1);copy[strlen(base)-1]=0;cJSON_ReplaceItemInObjectCaseSensitive(cfg,"base_url",cJSON_CreateString(copy));free(copy);}const char*cache=text(cfg,"cache_path");if(!cache||!cache[0]){char scope[65],context[512],path[2048];snprintf(context,sizeof(context),"%s\n%s",text(cfg,"base_url")?text(cfg,"base_url"):"",text(cfg,"product_id")?text(cfg,"product_id"):"");if(!hash(context,scope)){cJSON_Delete(cfg);return NULL;}
#ifdef _WIN32
 const char*root=getenv("LOCALAPPDATA");
#else
 const char*root=getenv("XDG_CACHE_HOME");
#endif
 if(!root||!root[0]){root=getenv("HOME");}
 if(!root||!root[0]){cJSON_Delete(cfg);return NULL;}snprintf(path,sizeof(path),"%s/Licentivo/%s/lease.json",root,scope);cJSON_DeleteItemFromObjectCaseSensitive(cfg,"cache_path");cJSON_AddStringToObject(cfg,"cache_path",path);}int64_t timeout=10;if(get(cfg,"timeout_seconds")&&(!num(cfg,"timeout_seconds",&timeout)||timeout<1||timeout>120)){cJSON_Delete(cfg);return NULL;}char dev[65];cJSON*profile=machine_profile(text(cfg,"product_id"),dev);if(!profile){cJSON_Delete(cfg);return NULL;}cJSON_DeleteItemFromObjectCaseSensitive(cfg,"device_profile");cJSON_AddItemToObject(cfg,"device_profile",profile);cJSON_DeleteItemFromObjectCaseSensitive(cfg,"device_id");cJSON_AddStringToObject(cfg,"device_id",dev);const char*secret=text(cfg,"license_key");const cJSON*keys=get(cfg,"trusted_keys");int valid=origin(cfg)&&device(text(cfg,"device_id"))&&uuid(text(cfg,"product_id"))&&secret&&(!secret[0]||(strlen(secret)==50&&(!strncmp(secret,"lv_lic_",7)||!strncmp(secret,"lv_tmp_",7))))&&cJSON_IsObject(keys)&&keys->child;for(const cJSON*k=keys?keys->child:NULL;k;k=k->next){unsigned char raw[32];if(!cJSON_IsString(k)||b64(k->valuestring,raw,32)!=32)valid=0;}if(!valid){cJSON_Delete(cfg);return NULL;}ln_client*c=calloc(1,sizeof(*c));if(!c){cJSON_Delete(cfg);return NULL;}if(!request_uuid(c->session)){cJSON_Delete(cfg);free(c);return NULL;}c->cfg=cfg;c->interval=3600;c->tick=monotonic();
#ifdef _WIN32
 InitializeCriticalSection(&c->mutex);InitializeCriticalSection(&c->view_mutex);
#else
 pthread_mutex_init(&c->mutex,NULL);pthread_mutex_init(&c->view_mutex,NULL);atomic_init(&c->stop,0);atomic_init(&c->denied,0);
#endif
 load_credential(c);load_logout_state(c);if(!source_input(c)[0]&&!c->auth&&!is_denied(c)){ln_destroy(c);return NULL;}const char*path=text(cfg,"cache_path");char*b=!is_denied(c)&&path&&path[0]?readfile(path,LIMIT):NULL;if(b){cJSON*s=ln_json(b);int64_t version,observed;char h[65];int hash_ok=source_hash(c,h);if(hash_ok&&s&&num(s,"version",&version)&&version==1&&text(s,"license_hash")&&!strcmp(text(s,"license_hash"),h)&&num(s,"observed_at",&observed)&&time(NULL)+300>=observed&&ln_verify(get(s,"lease"),keys,text(cfg,"product_id"),text(cfg,"device_id"),time(NULL))){int interval=3600,mode=envelope_policy(get(s,"lease"),&interval);if(mode!=1&&client_context(c,get(s,"lease"))){c->cached=cJSON_Duplicate(get(s,"lease"),1);c->wall=observed;c->interval=interval;c->offline=mode;}}cJSON_Delete(s);free(b);}publish(c,0);if(has_pending_logouts(c))ln_start_heartbeat(c);return c;}
ln_client*ln_init_file(const char*p){char*b=readfile(p,LIMIT);if(!b)return NULL;ln_client*c=ln_init(b);free(b);return c;}
ln_client*ln_init_product(const char*product_id,const char*trusted_keys_json,const char*license_key){
    if(!product_id||!trusted_keys_json||!license_key)return NULL;
    const char*start=license_key;while(*start==' '||*start=='\t'||*start=='\r'||*start=='\n')start++;
    const char*end=start+strlen(start);while(end>start&&(end[-1]==' '||end[-1]=='\t'||end[-1]=='\r'||end[-1]=='\n'))end--;
    size_t length=(size_t)(end-start);if(length>50)return NULL;
    char trimmed[51];memcpy(trimmed,start,length);trimmed[length]=0;
    cJSON*keys=ln_json(trusted_keys_json);if(!cJSON_IsObject(keys)){cJSON_Delete(keys);return NULL;}
    cJSON*cfg=cJSON_CreateObject();if(!cfg){cJSON_Delete(keys);return NULL;}
    cJSON_AddStringToObject(cfg,"product_id",product_id);cJSON_AddItemToObject(cfg,"trusted_keys",keys);cJSON_AddStringToObject(cfg,"license_key",trimmed);
    char*json=cJSON_PrintUnformatted(cfg);cJSON_Delete(cfg);if(!json)return NULL;
    ln_client*client=ln_init(json);cJSON_free(json);return client;
}
ln_client*ln_init_file_with_license(const char*path,const char*license_key){
 if(!license_key)return NULL;
 const char*start=license_key;while(*start==' '||*start=='\t'||*start=='\r'||*start=='\n')start++;
 const char*end=start+strlen(start);while(end>start&&(end[-1]==' '||end[-1]=='\t'||end[-1]=='\r'||end[-1]=='\n'))end--;
 size_t length=(size_t)(end-start);if(length>50)return NULL;
 char trimmed[51];memcpy(trimmed,start,length);trimmed[length]=0;
 char*b=readfile(path,LIMIT);if(!b)return NULL;
 cJSON*cfg=ln_json(b);free(b);if(!cfg)return NULL;
 cJSON_DeleteItemFromObjectCaseSensitive(cfg,"license_key");
 if(!cJSON_AddStringToObject(cfg,"license_key",trimmed)){cJSON_Delete(cfg);return NULL;}
 b=cJSON_PrintUnformatted(cfg);cJSON_Delete(cfg);if(!b)return NULL;
 ln_client*c=ln_init(b);free(b);return c;
}
const char*ln_last_error(const ln_client*c){return c?c->error:"INVALID_CONFIGURATION";}
static void invalidate(ln_client*c){cJSON_Delete(c->cached);c->cached=NULL;c->offline=0;c->interval=3600;publish(c,0);const char*p=text(c->cfg,"cache_path");if(p&&p[0]&&!is_denied(c))remove(p);}
static void mkdirs(const char*p){char*copy=malloc(strlen(p)+1);if(!copy)return;strcpy(copy,p);for(char*s=copy+1;*s;s++)if(*s=='/'||*s=='\\'){char old=*s;*s=0;mkdir_private(copy);*s=old;}free(copy);}
static int write_state(const char*p,const cJSON*s){
 char*b=cJSON_PrintUnformatted(s);char*tmp=malloc(strlen(p)+40);if(!tmp||!b){free(tmp);free(b);return 0;}unsigned char random[8];if(!random_bytes(random,8)){free(tmp);free(b);return 0;}
 /* Keep temporary filenames short even when a task journal has a long name. */
 const char*slash=strrchr(p,'/'),*backslash=strrchr(p,'\\');if(backslash&&(!slash||backslash>slash))slash=backslash;size_t prefix=slash?(size_t)(slash-p)+1:0;memcpy(tmp,p,prefix);sprintf(tmp+prefix,".state-%02x%02x%02x%02x%02x%02x%02x%02x.tmp",random[0],random[1],random[2],random[3],random[4],random[5],random[6],random[7]);mkdirs(p);FILE*f=fopen(tmp,"wb");int ok=0;if(f){
#ifndef _WIN32
 fchmod(fileno(f),0600);
#endif
 ok=fwrite(b,1,strlen(b),f)==strlen(b);if(fflush(f))ok=0;
#ifdef _WIN32
 if(ok&&_commit(_fileno(f)))ok=0;
#else
 if(ok&&fsync(fileno(f)))ok=0;
#endif
 if(fclose(f))ok=0;
#ifdef _WIN32
 if(ok)ok=state_windows_move(tmp,p);
#else
 if(ok)ok=rename(tmp,p)==0;
#endif
 }remove(tmp);free(tmp);free(b);return ok;
}
static int save(ln_client*c){const char*p=text(c->cfg,"cache_path");if(!p||!p[0])return 1;char h[65];if(!source_hash(c,h))return 0;cJSON*s=cJSON_CreateObject();cJSON_AddNumberToObject(s,"version",1);cJSON_AddStringToObject(s,"license_hash",h);cJSON_AddItemToObject(s,"lease",cJSON_Duplicate(c->cached,1));cJSON_AddNumberToObject(s,"observed_at",(double)c->wall);int ok=write_state(p,s);cJSON_Delete(s);return ok;}
static char*credential_path(ln_client*c){const char*p=text(c->cfg,"cache_path");if(!p||!p[0])return NULL;char*out=malloc(strlen(p)+18);if(out)sprintf(out,"%s.credential.json",p);return out;}
static void load_credential(ln_client*c){
 
 char*p=credential_path(c);if(!p)return;char*b=readfile(p,LIMIT);free(p);if(!b)return;cJSON*s=ln_json(b);free(b);char h[65];int64_t version;int ok=hash(source_input(c),h)&&s&&num(s,"version",&version)&&version==1&&text(s,"origin")&&!strcmp(text(s,"origin"),text(c->cfg,"base_url"))&&device(text(s,"source_hash"))&&(!source_input(c)[0]||!strcmp(text(s,"source_hash"),h))&&uuid(text(s,"request_id"))&&uuid(text(s,"session_id"));
 const char*ctx[]={"product_id","device_id"};for(int i=0;i<2;i++){const char*a=text(s,ctx[i]),*b=text(c->cfg,ctx[i]);if(!a||!b||strcmp(a,b))ok=0;}const char*k=text(s,"device_key");if(!k||(k[0]&&!credential_secret(k,"lv_dev_"))||(!text(c->cfg,"license_key")[0]&&!k[0]))ok=0;
 if(ok){c->auth=s;if(!k[0])strcpy(c->session,text(s,"session_id"));}else cJSON_Delete(s);
}
static int save_credential(ln_client*c){char*p=credential_path(c);if(!p){strcpy(c->error,"CREDENTIAL_PATH_REQUIRED");return 0;}int ok=write_state(p,c->auth);free(p);if(ok)c->auth_dirty=0;else strcpy(c->error,"CREDENTIAL_WRITE_FAILED");return ok;}
static int prepare_credential(ln_client*c){
 if(is_denied(c)){strcpy(c->error,"LAUNCH_LOGIN_REQUIRED");return 0;}if(!temporary(c))return 1;
 if(!c->auth){char h[65],id[37];if(!source_hash(c,h)||!request_uuid(id))return 0;c->auth=cJSON_CreateObject();cJSON_AddNumberToObject(c->auth,"version",1);cJSON_AddStringToObject(c->auth,"origin",text(c->cfg,"base_url"));cJSON_AddStringToObject(c->auth,"source_hash",h);cJSON_AddStringToObject(c->auth,"product_id",text(c->cfg,"product_id"));cJSON_AddStringToObject(c->auth,"device_id",text(c->cfg,"device_id"));cJSON_AddStringToObject(c->auth,"device_key","");cJSON_AddStringToObject(c->auth,"request_id",id);cJSON_AddStringToObject(c->auth,"session_id",c->session);c->auth_dirty=1;}
 return !c->auth_dirty||save_credential(c);
}
static int accept_credential(ln_client*c,const cJSON*result){
 if(logout_binding_blocked(c,result))return 0;
 if(cJSON_IsTrue(get(c->logout_state,"signed_out"))){cJSON_ReplaceItemInObjectCaseSensitive(c->logout_state,"signed_out",cJSON_CreateBool(0));if(!save_logout_state(c))return 0;}
 if(get(result,"logout_ticket")){if(!prepare_credential(c))return 0;cJSON_DeleteItemFromObjectCaseSensitive(c->auth,"logout_ticket");cJSON_AddItemToObject(c->auth,"logout_ticket",cJSON_Duplicate(get(result,"logout_ticket"),1));c->auth_dirty=1;if(!save_credential(c))return 0;}if(!temporary(c)||(text(c->auth,"device_key")&&text(c->auth,"device_key")[0]))return 1;
 const char*k=text(result,"device_key");if(!credential_secret(k,"lv_dev_")){if(strncmp(text(c->cfg,"license_key"),"lv_tmp_",7)&&!(text(c->cfg,"identity_token")&&text(c->cfg,"identity_token")[0]))return 1;strcpy(c->error,"DEVICE_CREDENTIAL_MISSING");return 0;}if(!prepare_credential(c))return 0;cJSON_ReplaceItemInObjectCaseSensitive(c->auth,"device_key",cJSON_CreateString(k));c->auth_dirty=1;return save_credential(c);
}
struct buffer{char data[LIMIT+1];size_t size;int oversized;};
static size_t receive(void*data,size_t size,size_t n,void*arg){struct buffer*b=arg;size_t amount=size*n;if(amount>LIMIT-b->size){b->oversized=1;return 0;}memcpy(b->data+b->size,data,amount);b->size+=amount;b->data[b->size]=0;return amount;}
static void sleepms(int ms){
#ifdef _WIN32
 Sleep((DWORD)ms);
#else
 struct timespec ts={ms/1000,(ms%1000)*1000000L};nanosleep(&ts,NULL);
#endif
}
static size_t receive_header(char*data,size_t size,size_t n,void*arg){size_t length=size*n;ln_client*c=arg;if(length>12&&!strncmp(data,"Retry-After:",12)){char value[32];size_t count=length-12;if(count>31)count=31;memcpy(value,data+12,count);value[count]=0;long seconds=strtol(value,NULL,10);if(seconds>0)c->retry_after=seconds>86400?86400:(int)seconds;}return length;}
static cJSON*request_extra(ln_client*c,const char*op,const char*activation,int*transient,const char*operation_id,const cJSON*extra){*transient=0;c->error[0]=0;c->request_id[0]=0;c->retryable=0;c->http_status=0;c->retry_after=0;if(!prepare_credential(c))return NULL;if(!strcmp(op,"activate")&&temporary(c)&&!text(c->auth,"device_key")[0]&&!c->cached)operation_id=text(c->auth,"request_id");unsigned char random[16];if(!random_bytes(random,16)){strcpy(c->error,"RANDOM_SOURCE_FAILED");return NULL;}char idem[120]="Idempotency-Key: ";size_t used=strlen(idem);for(int i=0;i<16;i++)sprintf(idem+used+i*2,"%02x",random[i]);if(operation_id){if(strlen(operation_id)>80||strpbrk(operation_id," \t\r\n")){strcpy(c->error,"INVALID_OPERATION_ID");return NULL;}snprintf(idem,sizeof(idem),"Idempotency-Key: %s",operation_id);}cJSON*body=cJSON_CreateObject();const char*names[]={"product_id","license_key","device_id","device_name","app_version"};for(int i=0;i<5;i++){const char*s=text(c->cfg,names[i]);cJSON_AddStringToObject(body,names[i],s?s:"");}cJSON_ReplaceItemInObjectCaseSensitive(body,"license_key",cJSON_CreateString(runtime_key(c)));cJSON_AddStringToObject(body,"activation_id",activation);cJSON_AddStringToObject(body,"session_id",c->session);cJSON_AddItemToObject(body,"device_profile",cJSON_Duplicate(get(c->cfg,"device_profile"),1));if(extra)for(const cJSON*x=extra->child;x;x=x->next)cJSON_AddItemToObject(body,x->string,cJSON_Duplicate(x,1));if(!get(c->auth,"logout_ticket"))cJSON_AddBoolToObject(body,"request_logout_ticket",1);if(text(c->cfg,"launch_connection_id")){cJSON_AddStringToObject(body,"launch_connection_id",text(c->cfg,"launch_connection_id"));cJSON_AddStringToObject(body,"environment",text(c->cfg,"environment")?text(c->cfg,"environment"):"");}if(!runtime_key(c)[0]&&text(c->cfg,"identity_token")){const char*identity_names[]={"identity_token","launch_connection_id","environment"};for(int i=0;i<3;i++){const char*v=text(c->cfg,identity_names[i]);cJSON_DeleteItemFromObjectCaseSensitive(body,identity_names[i]);cJSON_AddStringToObject(body,identity_names[i],v?v:"");}op="launch/exchange";}char*json=cJSON_PrintUnformatted(body);cJSON_Delete(body);const char*base=text(c->cfg,"base_url");char*url=malloc(strlen(base)+strlen(op)+20);if(!url||!json){free(url);free(json);return NULL;}strcpy(url,base);size_t len=strlen(url);if(len&&url[len-1]=='/')url[len-1]=0;strcat(url,"/api/v1/");strcat(url,op);struct curl_slist*headers=NULL;headers=curl_slist_append(headers,"Content-Type: application/json");headers=curl_slist_append(headers,idem);cJSON*result=NULL;
 for(int attempt=0;attempt<2;attempt++){if(attempt)sleepms(200);struct buffer b={{0},0,0};CURL*curl=curl_easy_init();if(!curl)break;curl_easy_setopt(curl,CURLOPT_URL,url);curl_easy_setopt(curl,CURLOPT_POSTFIELDS,json);curl_easy_setopt(curl,CURLOPT_HTTPHEADER,headers);curl_easy_setopt(curl,CURLOPT_WRITEFUNCTION,receive);curl_easy_setopt(curl,CURLOPT_WRITEDATA,&b);int64_t timeout=10;num(c->cfg,"timeout_seconds",&timeout);curl_easy_setopt(curl,CURLOPT_TIMEOUT,(long)timeout);const char*proxy=text(c->cfg,"proxy_url");if(proxy&&proxy[0])curl_easy_setopt(curl,CURLOPT_PROXY,proxy);curl_easy_setopt(curl,CURLOPT_HEADERFUNCTION,receive_header);curl_easy_setopt(curl,CURLOPT_HEADERDATA,c);curl_easy_setopt(curl,CURLOPT_CONNECTTIMEOUT,5L);curl_easy_setopt(curl,CURLOPT_NOSIGNAL,1L);curl_easy_setopt(curl,CURLOPT_FOLLOWLOCATION,0L);curl_easy_setopt(curl,CURLOPT_SSL_VERIFYPEER,1L);curl_easy_setopt(curl,CURLOPT_SSL_VERIFYHOST,2L);const char*ca=getenv("LICENTIVO_CA_BUNDLE");if(ca&&ca[0])curl_easy_setopt(curl,CURLOPT_CAINFO,ca);CURLcode err=curl_easy_perform(curl);long status=0;curl_easy_getinfo(curl,CURLINFO_RESPONSE_CODE,&status);curl_easy_cleanup(curl);if(b.oversized){strcpy(c->error,"RESPONSE_TOO_LARGE");*transient=0;break;}c->http_status=status;if(err!=CURLE_OK){*transient=1;c->retryable=1;strcpy(c->error,"SERVER_UNAVAILABLE");continue;}*transient=0;if(memchr(b.data,0,b.size)){strcpy(c->error,"INVALID_JSON");break;}cJSON*env=ln_json(b.data);if(!env){strcpy(c->error,"UNEXPECTED_RESPONSE");c->retryable=status==429||status>=500;*transient=c->retryable;if(c->retry_after>0)c->retry_at=(int64_t)time(NULL)+c->retry_after;if(status>=500)continue;break;}const char*trace=text(env,"request_id");snprintf(c->request_id,sizeof(c->request_id),"%s",trace?trace:"");if(status!=200||get(env,"error")){const char*code=text(get(env,"error"),"code");snprintf(c->error,sizeof(c->error),"%s",code?code:"UNEXPECTED_RESPONSE");c->retryable=status==429||status>=500;*transient=c->retryable;if(c->retry_after>0)c->retry_at=(int64_t)time(NULL)+c->retry_after;cJSON_Delete(env);if(status>=500)continue;break;}result=cJSON_Duplicate(get(env,"data"),1);cJSON_Delete(env);break;}
 curl_slist_free_all(headers);free(url);free(json);if(!result&&revocation_error(c->error))deny_runtime(c);return result;
}
static cJSON*request(ln_client*c,const char*op,const char*activation,int*transient){return request_extra(c,op,activation,transient,NULL,NULL);}
#include "device_logout.h"
static char*activation_id(ln_client*c){if(!c->cached)return NULL;unsigned char raw[LIMIT+1];int n=b64(text(c->cached,"payload"),raw,LIMIT);if(n<0)return NULL;raw[n]=0;cJSON*claims=ln_json((char*)raw);const char*s=text(claims,"activation_id");char*out=s?malloc(strlen(s)+1):NULL;if(out)strcpy(out,s);cJSON_Delete(claims);return out;}
static int call(ln_client*c,const char*op,int force_online){if(!c)return 0;lock(c);if(is_denied(c)){strcpy(c->error,"LAUNCH_LOGIN_REQUIRED");unlock(c);return 0;}if(!force_online&&!strcmp(op,"activate")&&c->offline==2&&c->interval==0&&cache_valid(c)){unlock(c);return 1;}char*id=NULL;if(strcmp(op,"activate")){id=activation_id(c);if(!id){strcpy(c->error,"ACTIVATE_FIRST");unlock(c);return 0;}}int temporary=0;cJSON*data=request(c,op,id?id:"",&temporary);free(id);if(!data){int ok=!force_online&&temporary&&c->offline!=1&&cache_valid(c);if(!temporary||c->offline==1)invalidate(c);publish(c,0);unlock(c);notify(c);return ok;}
 if(is_denied(c)){if(c->auth&&get(data,"logout_ticket")){cJSON_DeleteItemFromObjectCaseSensitive(c->auth,"logout_ticket");cJSON_AddItemToObject(c->auth,"logout_ticket",cJSON_Duplicate(get(data,"logout_ticket"),1));}cJSON_Delete(data);unlock(c);return 0;}const cJSON*e=get(data,"lease");int ok=client_context(c,e)&&ln_verify(e,get(c->cfg,"trusted_keys"),text(c->cfg,"product_id"),text(c->cfg,"device_id"),time(NULL));if(ok){cJSON_Delete(c->cached);c->cached=cJSON_Duplicate(e,1);char*activation=activation_id(c);const char*expected=text(data,"activation_id");ok=activation&&expected&&!strcmp(activation,expected);free(activation);}if(ok){c->wall=time(NULL);c->tick=monotonic();int64_t interval;if(num(data,"heartbeat_seconds",&interval)&&interval>=10&&interval<=86400)c->interval=(int)interval;c->offline=envelope_policy(c->cached,&c->interval);ok=accept_credential(c,data)&&save(c);if(ok)c->error[0]=0;else if(c->error[0]==0)strcpy(c->error,"CACHE_WRITE_FAILED");}else{strcpy(c->error,"INVALID_LEASE");invalidate(c);}cJSON_Delete(data);publish(c,ok);unlock(c);notify(c);return ok;}
int ln_activate(ln_client*c){return call(c,"activate",0);}int ln_validate(ln_client*c){return call(c,"validate",0);}int ln_heartbeat(ln_client*c){return call(c,"heartbeat",0);}
int ln_check_feature(ln_client*c,const char*feature){if(!c||!feature)return 0;cJSON*claims=local_claims(c);int ok=0;const cJSON*fs=get(claims,"features");for(const cJSON*f=fs?fs->child:NULL;f;f=f->next)if(cJSON_IsString(f)&&!strcmp(f->valuestring,feature))ok=1;cJSON_Delete(claims);return ok;}
int ln_run_feature(ln_client*c,const char*feature,ln_feature_action action,void*context){if(!action||!ln_check_feature(c,feature))return 0;return action(context)!=0;}
int ln_refresh_online(ln_client*c){return call(c,"activate",1);}
int ln_deactivate(ln_client*c){return ln_logout(c);}
static int stopped(ln_client*c){
#ifdef _WIN32
 return InterlockedCompareExchange(&c->stop,0,0)!=0;
#else
 return atomic_load(&c->stop);
#endif
}
#ifdef _WIN32
static DWORD WINAPI heartbeat_run(LPVOID arg){
#else
static void*heartbeat_run(void*arg){
#endif
 ln_client*c=arg;lock(c);int refresh=c->refresh_pending;c->refresh_pending=0;unlock(c);if(refresh&&!stopped(c))ln_refresh_online(c);while(!stopped(c)){unsigned char r=0;random_bytes(&r,1);lock(c);double wait=has_pending_logouts(c)?2:(c->interval<600?600:c->interval)*(1+r/255.0*.1);double retry=(double)c->retry_at-time(NULL);if(retry>wait)wait=retry;int disabled=(c->interval==0||is_denied(c))&&!has_pending_logouts(c);unlock(c);if(disabled)break;double end=monotonic()+wait;while(!stopped(c)&&monotonic()<end)sleepms(100);if(!stopped(c)){if(is_denied(c)||ln_logout_pending(c))ln_retry_pending_logouts(c);if(!is_denied(c))ln_heartbeat(c);}}lock(c);c->finished=1;unlock(c);return 0;}
int ln_start_heartbeat(ln_client*c){if(!c)return 0;if(c->started){lock(c);int finished=c->finished;unlock(c);if(!finished)return 1;ln_stop_heartbeat(c);}lock(c);int disabled=c->interval==0;c->finished=0;unlock(c);if(disabled&&!c->refresh_pending)return 1;
#ifdef _WIN32
 InterlockedExchange(&c->stop,0);c->thread=CreateThread(NULL,0,heartbeat_run,c,0,NULL);if(!c->thread)return 0;
#else
 atomic_store(&c->stop,0);if(pthread_create(&c->thread,NULL,heartbeat_run,c))return 0;
#endif
 c->started=1;return 1;}
void ln_stop_heartbeat(ln_client*c){if(!c||!c->started)return;
#ifdef _WIN32
 InterlockedExchange(&c->stop,1);WaitForSingleObject(c->thread,INFINITE);CloseHandle(c->thread);
#else
 atomic_store(&c->stop,1);pthread_join(c->thread,NULL);
#endif
 c->started=0;}
void ln_destroy(ln_client*c){if(!c)return;ln_stop_heartbeat(c);if(floating(c)&&!is_denied(c)){char*id=activation_id(c);if(id){int transient;cJSON*result=request(c,"deactivate",id,&transient);cJSON_Delete(result);free(id);}}cJSON_Delete(c->logout_bindings);cJSON_Delete(c->logout_state);cJSON_Delete(c->cfg);cJSON_Delete(c->cached);cJSON_Delete(c->auth);cJSON_Delete(c->view);
#ifdef _WIN32
 DeleteCriticalSection(&c->mutex);DeleteCriticalSection(&c->view_mutex);
#else
 pthread_mutex_destroy(&c->mutex);pthread_mutex_destroy(&c->view_mutex);
#endif
 free(c);}

int ln_start(ln_client*c){if(!c)return 0;if(is_denied(c)&&ln_logout_pending(c))ln_retry_pending_logouts(c);lock(c);int fast=cache_valid(c)&&c->offline!=1&&!floating(c);if(fast)c->refresh_pending=1;unlock(c);return (fast||ln_activate(c))&&ln_start_heartbeat(c);}
ln_client*ln_open(const char*path){ln_client*c=ln_init_file(path);if(c&&!ln_start(c)){ln_destroy(c);return NULL;}return c;}
ln_client*ln_open_with_license(const char*path,const char*license_key){ln_client*c=ln_init_file_with_license(path,license_key);if(c&&!ln_start(c)){ln_destroy(c);return NULL;}return c;}
void ln_free_string(char*s){free(s);}
char*ln_consume(ln_client*c,const char*feature,int64_t quantity,const char*operation_id){if(!c||!feature||!operation_id||!operation_id[0])return NULL;lock(c);if(!cache_valid(c)){strcpy(c->error,"LICENSE_CHECK_REQUIRED");unlock(c);return NULL;}char*activation=activation_id(c);cJSON*extra=cJSON_CreateObject();cJSON_AddStringToObject(extra,"feature",feature);cJSON_AddNumberToObject(extra,"quantity",(double)quantity);int transient=0;cJSON*result=request_extra(c,"consume",activation,&transient,operation_id,extra);char*out=result?cJSON_PrintUnformatted(get(result,"consumption")):NULL;cJSON_Delete(extra);free(activation);if(!result&&(!strcmp(c->error,"LICENSE_REVOKED")||!strcmp(c->error,"LICENSE_SUSPENDED")||!strcmp(c->error,"DEVICE_RELEASED")||!strcmp(c->error,"DEVICE_CREDENTIAL_REVOKED")||!strcmp(c->error,"LICENSE_EXPIRED")))invalidate(c);int success=result!=NULL;cJSON_Delete(result);publish(c,success);unlock(c);notify(c);return out;}
char*ln_offline_request(ln_client*c,const char*operation){if(!c||is_denied(c)||!operation||(strcmp(operation,"activate")&&strcmp(operation,"deactivate")))return NULL;lock(c);if(!prepare_credential(c)){unlock(c);return NULL;}char*activation=NULL;if(!strcmp(operation,"deactivate")){if(!cache_valid(c)){unlock(c);return NULL;}activation=activation_id(c);invalidate(c);}char id[37];if(!request_uuid(id)){free(activation);unlock(c);return NULL;}cJSON*body=cJSON_CreateObject();const char*names[]={"product_id","license_key","device_id","device_name","app_version"};for(int i=0;i<5;i++){const char*v=text(c->cfg,names[i]);cJSON_AddStringToObject(body,names[i],v?v:"");}cJSON_ReplaceItemInObjectCaseSensitive(body,"license_key",cJSON_CreateString(runtime_key(c)));cJSON_AddStringToObject(body,"activation_id",activation?activation:"");cJSON_AddStringToObject(body,"session_id",c->session);cJSON_AddItemToObject(body,"device_profile",cJSON_Duplicate(get(c->cfg,"device_profile"),1));cJSON_AddBoolToObject(body,"request_logout_ticket",1);cJSON_AddNumberToObject(body,"version",1);cJSON_AddStringToObject(body,"request_id",id);cJSON_AddNumberToObject(body,"created_at",(double)time(NULL));cJSON_AddStringToObject(body,"operation",operation);char*out=cJSON_PrintUnformatted(body);cJSON_Delete(body);free(activation);unlock(c);return out;}
int ln_import_offline(ln_client*c,const char*request_json,const char*response_json){if(!c||is_denied(c)||!request_json||!response_json||strlen(request_json)>LIMIT||strlen(response_json)>LIMIT)return 0;lock(c);cJSON*req=ln_json(request_json),*res=ln_json(response_json);int64_t version=0;int ok=req&&res&&num(res,"version",&version)&&version==1;const char*fields[]={"request_id","operation"};for(int i=0;i<2;i++){const char*a=text(req,fields[i]),*b=text(res,fields[i]);if(!a||!b||strcmp(a,b))ok=0;}if(!text(req,"operation")||strcmp(text(req,"operation"),"activate"))ok=0;const char*context[]={"product_id","device_id","license_key"};for(int i=0;i<3;i++){const char*a=text(req,context[i]),*b=!strcmp(context[i],"license_key")?runtime_key(c):text(c->cfg,context[i]);if(!a||!b||strcmp(a,b))ok=0;}
 const cJSON*result=get(res,"result"),*envelope=get(result,"lease");if(!client_context(c,envelope)||!ln_verify(envelope,get(c->cfg,"trusted_keys"),text(c->cfg,"product_id"),text(c->cfg,"device_id"),time(NULL)))ok=0;cJSON*claims=claims_from(envelope);const char*id=text(claims,"offline_request_id"),*expected=text(req,"request_id"),*activation=text(claims,"activation_id"),*response_id=text(result,"activation_id");if(!id||!expected||strcmp(id,expected)||!activation||!response_id||strcmp(activation,response_id)||envelope_policy(envelope,NULL)==1)ok=0;
 if(ok){cJSON_Delete(c->cached);c->cached=cJSON_Duplicate(envelope,1);c->wall=time(NULL);c->tick=monotonic();c->offline=envelope_policy(c->cached,&c->interval);ok=accept_credential(c,result)&&save(c);}if(!ok)strcpy(c->error,"INVALID_OFFLINE_RESPONSE");cJSON_Delete(claims);cJSON_Delete(req);cJSON_Delete(res);publish(c,0);unlock(c);notify(c);return ok;
}

static char*reservation_call(ln_client*c,const char*op,const char*operation_id,const cJSON*extra){if(!c||!operation_id||!operation_id[0])return NULL;lock(c);if(!cache_valid(c)){strcpy(c->error,"LICENSE_CHECK_REQUIRED");publish(c,0);unlock(c);return NULL;}char*activation=activation_id(c);int transient=0;cJSON*result=request_extra(c,op,activation,&transient,operation_id,extra);char*out=result?cJSON_PrintUnformatted(get(result,"reservation")):NULL;if(!result&&(!strcmp(c->error,"LICENSE_REVOKED")||!strcmp(c->error,"LICENSE_SUSPENDED")||!strcmp(c->error,"DEVICE_RELEASED")||!strcmp(c->error,"DEVICE_CREDENTIAL_REVOKED")||!strcmp(c->error,"LICENSE_EXPIRED")))invalidate(c);publish(c,result!=NULL);free(activation);cJSON_Delete(result);unlock(c);notify(c);return out;}
char*ln_reserve(ln_client*c,const char*feature,int64_t quantity,const char*operation_id){if(!feature)return NULL;cJSON*extra=cJSON_CreateObject();cJSON_AddStringToObject(extra,"feature",feature);cJSON_AddNumberToObject(extra,"quantity",(double)quantity);char*out=reservation_call(c,"consume/reserve",operation_id,extra);cJSON_Delete(extra);return out;}
static char*finish_reservation(ln_client*c,const char*op,const char*id,const char*operation_id){if(!id)return NULL;cJSON*extra=cJSON_CreateObject();cJSON_AddStringToObject(extra,"reservation_id",id);char*out=reservation_call(c,op,operation_id,extra);cJSON_Delete(extra);return out;}
char*ln_commit(ln_client*c,const char*id,const char*operation_id){return finish_reservation(c,"consume/commit",id,operation_id);}
char*ln_cancel(ln_client*c,const char*id,const char*operation_id){return finish_reservation(c,"consume/cancel",id,operation_id);}
#include "feature_tasks.h"
