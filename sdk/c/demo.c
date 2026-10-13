#define _POSIX_C_SOURCE 200809L
#ifdef _WIN32
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0600
#endif
#endif
#include "licentivo.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <time.h>
#ifdef _WIN32
#include <windows.h>
static double clock_seconds(void){return GetTickCount64()/1000.0;}
#else
static double clock_seconds(void){struct timespec t;clock_gettime(CLOCK_MONOTONIC,&t);return t.tv_sec+t.tv_nsec/1e9;}
#endif
#include "vendor/cJSON.h"
static char *read_text(const char *path) { FILE*f=fopen(path,"rb");if(!f)return NULL;char*b=calloc(32769,1);size_t n=fread(b,1,32768,f);int bad=ferror(f)||!feof(f);fclose(f);if(bad||!n){free(b);return NULL;}return b;}
static int used_is(const char*s,double used,double over){cJSON*j=s?cJSON_Parse(s):NULL;const cJSON*u=cJSON_GetObjectItemCaseSensitive(j,"used"),*o=cJSON_GetObjectItemCaseSensitive(j,"overage");int ok=cJSON_IsNumber(u)&&u->valuedouble==used&&cJSON_IsNumber(o)&&o->valuedouble==over;cJSON_Delete(j);return ok;}

static cJSON*receipt(char*value){if(!value)return NULL;cJSON*j=ln_json(value);ln_free_string(value);return j;}
static int reservation_is(const cJSON*j,const char*status,double used){const cJSON*s=cJSON_GetObjectItemCaseSensitive(j,"status"),*cons=cJSON_GetObjectItemCaseSensitive(j,"consumption"),*u=cJSON_GetObjectItemCaseSensitive(cons,"used");return cJSON_IsString(s)&&!strcmp(s->valuestring,status)&&cJSON_IsNumber(u)&&u->valuedouble==used;}
static int improvements(ln_client*c,const char*path){int ok=ln_start(c);cJSON*one=ok?receipt(ln_reserve(c,"export",1,"cancel-job")):NULL;ok=ok&&reservation_is(one,"pending",0);const cJSON*id=cJSON_GetObjectItemCaseSensitive(one,"reservation_id");cJSON*cancel=ok?receipt(ln_cancel(c,id->valuestring,"cancel-job")):NULL;ok=ok&&reservation_is(cancel,"canceled",0);cJSON_Delete(cancel);cJSON_Delete(one);cJSON*two=ok?receipt(ln_reserve(c,"export",1,"commit-job")):NULL;id=cJSON_GetObjectItemCaseSensitive(two,"reservation_id");for(int i=0;i<2&&ok;i++){cJSON*done=receipt(ln_commit(c,id->valuestring,"commit-job"));ok=reservation_is(done,"committed",1);cJSON_Delete(done);}cJSON_Delete(two);cJSON*replay=ok?receipt(ln_reserve(c,"export",1,"commit-job")):NULL;ok=ok&&reservation_is(replay,"committed",1);cJSON_Delete(replay);char*over=ok?ln_consume(c,"export",1000,"quota-job"):NULL;ln_error_info e={0};ln_get_error_info(c,&e);ok=ok&&!over&&!strcmp(e.code,"FEATURE_QUOTA_EXCEEDED")&&e.request_id[0]&&!e.retryable&&ln_check_feature(c,"export");ln_free_string(over);ln_client*restored=ok?ln_init_file_with_license(path,""):NULL;ok=ok&&restored&&ln_start(restored)&&ln_validate(restored)&&ln_check_feature(restored,"export")&&ln_deactivate(restored)&&!ln_check_feature(restored,"export");ln_destroy(restored);return ok;}
int main(int argc, char **argv) {
    const char *path = argc > 2 && !strcmp(argv[1], "--config") ? argv[2] : ".local/sdk-demo.json";
    int cache_test = 0;
    for (int i = 1; i < argc; i++) if (!strcmp(argv[i], "--cache-test")) cache_test = 1;
    ln_client *client = ln_init_file(path);
    if (!client) { fprintf(stderr, "C: invalid configuration\n"); return 1; }
    for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--improvements-test")){int ok=improvements(client,path);if(!ok)fprintf(stderr,"C: %s\n",ln_last_error(client));ln_destroy(client);if(ok)puts("C: credential / reservation / typed status OK");return ok?0:1;}
    for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--startup-test")){double start=clock_seconds();int ok=ln_start(client)&&clock_seconds()-start<.3;start=clock_seconds();ok=ok&&ln_check_feature(client,"export")&&clock_seconds()-start<.3;ln_destroy(client);if(ok)puts("C: cached startup and local feature checks stay responsive");return ok?0:1;}
    int advanced=0,offline_export=0,offline_import=0,floating=0;
    for(int i=1;i<argc;i++){if(!strcmp(argv[i],"--advanced-test"))advanced=1;if(!strcmp(argv[i],"--offline-export"))offline_export=1;if(!strcmp(argv[i],"--offline-import"))offline_import=1;if(!strcmp(argv[i],"--floating-test"))floating=1;}
    if(offline_export){char*request=ln_offline_request(client,"activate");char file[4096];snprintf(file,sizeof(file),"%s.request.json",path);FILE*f=request?fopen(file,"wb"):NULL;int ok=f&&fwrite(request,1,strlen(request),f)==strlen(request);if(f)fclose(f);ln_free_string(request);ln_destroy(client);if(ok)puts("C: offline request exported");return ok?0:1;}
    for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--transfer-test")||!strcmp(argv[i],"--transfer-import")){
        int ok=1;if(!strcmp(argv[i],"--transfer-import")){char file[4096];snprintf(file,sizeof(file),"%s.request.json",path);char*req=read_text(file);snprintf(file,sizeof(file),"%s.response.json",path);char*res=read_text(file);ok=req&&res&&ln_import_offline(client,req,res);free(req);free(res);}else ok=ln_start(client);
        ok=ok&&ln_validate(client)&&ln_heartbeat(client)&&ln_check_feature(client,"export");if(!ok)fprintf(stderr,"C: %s\n",ln_last_error(client));ln_destroy(client);if(ok)puts("C: transferred device credential OK");return ok?0:1;
    }
    if(floating){int ok=ln_start(client);ln_client*other=ln_init_file(path);ok=ok&&other&&!ln_check_feature(other,"export")&&!ln_start(other);ln_destroy(client);ok=ok&&ln_start(other)&&ln_check_feature(other,"export");ln_destroy(other);if(ok)puts("C: floating seat release OK");return ok?0:1;}
    if(advanced||offline_import){int ok=1;
        if(offline_import){char file[4096];snprintf(file,sizeof(file),"%s.request.json",path);char*req=read_text(file);snprintf(file,sizeof(file),"%s.response.json",path);char*res=read_text(file);ok=req&&res&&ln_import_offline(client,req,res);free(req);free(res);}else ok=ln_start(client);
        ok=ok&&ln_check_feature(client,"export");char*r=ok?ln_consume(client,"export",1,"business-job-1"):NULL;ok=ok&&used_is(r,1,0);ln_free_string(r);
        r=ok?ln_consume(client,"export",1,"business-job-1"):NULL;ok=ok&&used_is(r,1,0);ln_free_string(r);
        r=ok?ln_consume(client,"export",3,"business-job-2"):NULL;ok=ok&&used_is(r,4,1);ln_free_string(r);
        r=ok?ln_consume(client,"export",1,"business-job-3"):NULL;ok=ok&&!r&&strstr(ln_last_error(client),"FEATURE_QUOTA_EXCEEDED")&&ln_check_feature(client,"export");ln_free_string(r);
        ok=ok&&ln_deactivate(client);if(!ok)fprintf(stderr,"C: %s\n",ln_last_error(client));ln_destroy(client);if(ok)puts("C: offline/metered feature checks OK");return ok?0:1;
    }
    for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--refresh-test")){
        int ok=!ln_refresh_online(client)&&ln_check_feature(client,"export");
        ok=ok&&ln_refresh_online(client)&&ln_check_feature(client,"updated")&&!ln_check_feature(client,"export");
        ok=ok&&!ln_refresh_online(client)&&!ln_check_feature(client,"updated");
        ln_destroy(client);if(ok)puts("C: online refresh OK");return ok?0:1;
    }
    int policy_test=0,deny=0;
    for(int i=1;i<argc;i++){if(!strcmp(argv[i],"--policy-test"))policy_test=1;if(!strcmp(argv[i],"--expect-denied"))deny=1;}
    if(policy_test){int ok=ln_check_feature(client,"export")!=deny;if(ok&&!deny)ok=ln_activate(client)&&ln_validate(client)&&ln_heartbeat(client);ln_destroy(client);if(ok)puts("C: policy cache OK");return ok?0:1;}
    int ok = ln_activate(client);
    if (ok && cache_test) {
        ln_destroy(client); client = ln_init_file(path);
        ok = client && ln_validate(client) && ln_check_feature(client, "export");
        if (ok) ok = !ln_validate(client) && !ln_check_feature(client, "export");
        ln_destroy(client); client = ln_init_file(path);
        ok = ok && client && !ln_check_feature(client, "export");
        if (ok) puts("C: cache restored on 503; 403 invalidated access and persisted cache");
    } else if (ok) {
        ok = ln_validate(client) && ln_heartbeat(client);
        if (ok) {
            printf("C: activated, validated, heartbeat OK; export enabled %d\n", ln_check_feature(client, "export"));
            ok = ln_deactivate(client);
        }
        if (ok) puts("C: deactivated");
    }
    if (!ok) fprintf(stderr, "C: %s\n", ln_last_error(client));
    ln_destroy(client);
    return ok ? 0 : 1;
}
