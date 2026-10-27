/* Internal implementation, included after the transport helpers in licentivo.c. */
static char*logout_path(ln_client*c){const char*p=text(c->cfg,"cache_path");if(!p)return NULL;char*out=malloc(strlen(p)+13);if(out)sprintf(out,"%s.logout.json",p);return out;}
static cJSON*pending_logouts(ln_client*c){return (cJSON*)get(c->logout_state,"pending");}
static int has_pending_logouts(ln_client*c){return cJSON_GetArraySize(pending_logouts(c))>0;}
static void new_logout_state(ln_client*c){c->logout_state=cJSON_CreateObject();cJSON_AddNumberToObject(c->logout_state,"version",1);cJSON_AddStringToObject(c->logout_state,"source_hash","");cJSON_AddBoolToObject(c->logout_state,"signed_out",0);cJSON_AddArrayToObject(c->logout_state,"pending");}
static const char*logout_text(const cJSON*s,const char*key){const char*v=text(s,key);return v?v:"";}
static void logout_binding(const cJSON*t,char out[80]){int64_t generation=0;const char*a=text(t,"activation_id");if(a&&num(t,"expected_generation",&generation))snprintf(out,80,"%s:%lld",a,(long long)generation);else out[0]=0;}
static char*logout_directory(ln_client*c){
 const char*root=getenv("LOCALAPPDATA");if(!root||!root[0])root=getenv("XDG_CACHE_HOME");char*owned=NULL;
 if(!root||!root[0]){const char*home=getenv("HOME");if(!home||!home[0])return NULL;owned=malloc(strlen(home)+8);if(!owned)return NULL;sprintf(owned,"%s/.cache",home);root=owned;}
 const char*cache=text(c->cfg,"cache_path");char*absolute=NULL;
#ifdef _WIN32
 wchar_t*wide=state_windows_path(cache);if(wide){const wchar_t*start=!wcsncmp(wide,L"\\\\?\\",4)?wide+4:wide;int n=WideCharToMultiByte(CP_UTF8,0,start,-1,NULL,0,NULL,NULL);absolute=malloc((size_t)n);if(absolute)WideCharToMultiByte(CP_UTF8,0,start,-1,absolute,n,NULL,NULL);free(wide);}
#else
 if(cache[0]=='/')absolute=strdup(cache);else{char*cwd=getcwd(NULL,0);if(cwd){absolute=malloc(strlen(cwd)+strlen(cache)+2);if(absolute)sprintf(absolute,"%s/%s",cwd,cache);free(cwd);}}
#endif
 if(!absolute){free(owned);return NULL;}size_t n=strlen(absolute)+strlen(text(c->cfg,"base_url"))+strlen(text(c->cfg,"product_id"))+strlen(text(c->cfg,"device_id"))+4;char*context=malloc(n);char scope[65];
 if(!context){free(absolute);free(owned);return NULL;}snprintf(context,n,"%s\n%s\n%s\n%s",absolute,text(c->cfg,"base_url"),text(c->cfg,"product_id"),text(c->cfg,"device_id"));int ok=hash(context,scope);free(absolute);free(context);char*out=ok?malloc(strlen(root)+strlen(scope)+20):NULL;if(out)sprintf(out,"%s/Licentivo/logout/%s",root,scope);free(owned);return out;
}
static char*logout_scope(const cJSON*s){const char*a=logout_text(s,"source_hash"),*b=logout_text(s,"binding_key");char*out=malloc(strlen(a)+strlen(b)+2);if(out)sprintf(out,"%s\n%s",a,b);return out;}
static int save_logout_state(ln_client*c){
 int64_t revision=0;num(c->logout_state,"revision",&revision);int64_t now=(int64_t)time(NULL)*1000;revision=revision+1>now?revision+1:now;cJSON_DeleteItemFromObjectCaseSensitive(c->logout_state,"revision");cJSON_AddNumberToObject(c->logout_state,"revision",(double)revision);
 char*dir=logout_directory(c),*scope=logout_scope(c->logout_state),h[65];int fallback=0;if(dir&&scope&&hash(scope,h)){char*p=malloc(strlen(dir)+72);if(p){sprintf(p,"%s/%s.json",dir,h);fallback=write_state(p,c->logout_state);free(p);}}free(dir);free(scope);
 char*p=logout_path(c);int primary=p&&write_state(p,c->logout_state);free(p);return fallback||(cJSON_IsTrue(get(c->logout_state,"signed_out"))&&primary);
}
static void read_logout_record(ln_client*c,cJSON*records,const char*path){
 errno=0;char*b=readfile(path,65536);int absent=errno==ENOENT;if(!b){if(!absent)set_denied(c);return;}cJSON*s=ln_json(b);free(b);int64_t version;
 if(!s||!num(s,"version",&version)||version!=1||!cJSON_IsArray(get(s,"pending"))){cJSON_Delete(s);set_denied(c);return;}char*scope=logout_scope(s);if(scope){const cJSON*old=get(records,scope);int64_t before=0,after=0;num(old,"revision",&before);num(s,"revision",&after);if(!old||after>before||(after==before&&(cJSON_IsTrue(get(s,"signed_out"))||!cJSON_IsTrue(get(old,"signed_out"))))){cJSON_DeleteItemFromObjectCaseSensitive(records,scope);cJSON_AddItemToObject(records,scope,s);}else cJSON_Delete(s);free(scope);}else cJSON_Delete(s);
}
static cJSON*logout_records(ln_client*c){
 cJSON*records=cJSON_CreateObject();char*p=logout_path(c);if(p){read_logout_record(c,records,p);free(p);}else set_denied(c);char*dir=logout_directory(c);
 if(dir){
#ifdef _WIN32
  char*pattern=malloc(strlen(dir)+8);if(pattern){sprintf(pattern,"%s/*.json",dir);wchar_t*wide=state_windows_path(pattern);free(pattern);WIN32_FIND_DATAW data;HANDLE search=wide?FindFirstFileW(wide,&data):INVALID_HANDLE_VALUE;free(wide);if(search!=INVALID_HANDLE_VALUE){do{char name[256];int n=WideCharToMultiByte(CP_UTF8,0,data.cFileName,-1,name,sizeof(name),NULL,NULL);if(n>0){p=malloc(strlen(dir)+strlen(name)+2);if(p){sprintf(p,"%s/%s",dir,name);read_logout_record(c,records,p);free(p);}}}while(FindNextFileW(search,&data));FindClose(search);}else if(GetLastError()!=ERROR_FILE_NOT_FOUND&&GetLastError()!=ERROR_PATH_NOT_FOUND)set_denied(c);}
#else
  DIR*entries=opendir(dir);if(entries){struct dirent*entry;while((entry=readdir(entries))){size_t n=strlen(entry->d_name);if(n>5&&!strcmp(entry->d_name+n-5,".json")){p=malloc(strlen(dir)+n+2);if(p){sprintf(p,"%s/%s",dir,entry->d_name);read_logout_record(c,records,p);free(p);}}}closedir(entries);}else if(errno!=ENOENT)set_denied(c);
#endif
  free(dir);
 }
 return records;
}
static void load_logout_state(ln_client*c){
 new_logout_state(c);c->logout_bindings=cJSON_CreateObject();cJSON*records=logout_records(c);
 char h[65]="",binding[80];source_hash(c,h);logout_binding(get(c->auth,"logout_ticket"),binding);cJSON*tickets=cJSON_CreateArray();
 for(cJSON*s=records->child;s;s=s->next){const char*b=logout_text(s,"binding_key");if(b[0]&&cJSON_IsTrue(get(s,"signed_out"))&&!get(c->logout_bindings,b))cJSON_AddBoolToObject(c->logout_bindings,b,1);}
 for(cJSON*s=records->child;s;s=s->next){const char*b=logout_text(s,"binding_key");if((!strcmp(h,logout_text(s,"source_hash"))||(logout_text(c->cfg,"launch_connection_id")[0]&&binding[0]&&!strcmp(b,binding))||(!c->auth&&!source_input(c)[0]))&&(!b[0]||!binding[0]||!strcmp(b,binding))){cJSON_Delete(c->logout_state);c->logout_state=cJSON_Duplicate(s,1);if(cJSON_IsTrue(get(s,"signed_out"))&&!text(c->cfg,"license_key")[0])set_denied(c);}for(const cJSON*t=get(s,"pending")->child;t;t=t->next){int found=0;for(cJSON*x=tickets->child;x;x=x->next)if(!strcmp(logout_text(t,"operation_id"),logout_text(x,"operation_id")))found=1;if(!found)cJSON_AddItemToArray(tickets,cJSON_Duplicate(t,1));}}
 cJSON_ReplaceItemInObjectCaseSensitive(c->logout_state,"pending",tickets);cJSON_Delete(records);
 if(is_denied(c)||cJSON_IsTrue(get(c->logout_state,"signed_out"))){cJSON_Delete(c->auth);c->auth=NULL;}
}
static int acknowledge_logout(ln_client*c,const char*operation){
 cJSON*records=logout_records(c);char*dir=logout_directory(c);int ok=dir!=NULL;
 for(cJSON*s=records->child;s&&ok;s=s->next){cJSON*pending=(cJSON*)get(s,"pending");int changed=0;for(int i=cJSON_GetArraySize(pending)-1;i>=0;i--){if(!strcmp(logout_text(cJSON_GetArrayItem(pending,i),"operation_id"),operation)){cJSON_DeleteItemFromArray(pending,i);changed=1;}}
  if(changed){char*scope=logout_scope(s),h[65];ok=scope&&hash(scope,h);if(ok){char*p=malloc(strlen(dir)+72);ok=p!=NULL;if(p){sprintf(p,"%s/%s.json",dir,h);ok=write_state(p,s);free(p);}}free(scope);}
 }
 free(dir);cJSON_Delete(records);return ok;
}
static int revocation_error(const char*code){const char*codes[]={"LICENSE_REVOKED","LICENSE_SUSPENDED","LICENSE_EXPIRED","DEVICE_RELEASED","DEVICE_NOT_ACTIVE","DEVICE_CREDENTIAL_REVOKED","DEVICE_CREDENTIAL_INVALID","AUTHORIZATION_SESSION_REVOKED","AUTHORIZATION_SESSION_EXPIRED","LAUNCH_LOGIN_REQUIRED","LAUNCH_ENTITLEMENT_REVOKED","LAUNCH_ENTITLEMENT_EXPIRED"};for(size_t i=0;i<sizeof(codes)/sizeof(codes[0]);i++)if(!strcmp(code,codes[i]))return 1;return 0;}
static int deny_runtime_mode(ln_client*c,int queue_release){
 set_denied(c);if(!c->logout_state)new_logout_state(c);char h[65];
 if((source_input(c)[0]||c->auth)&&source_hash(c,h))cJSON_ReplaceItemInObjectCaseSensitive(c->logout_state,"source_hash",cJSON_CreateString(h));
 cJSON_ReplaceItemInObjectCaseSensitive(c->logout_state,"signed_out",cJSON_CreateBool(1));
 cJSON_DeleteItemFromObjectCaseSensitive(c->logout_state,"offline_request_id");cJSON_AddStringToObject(c->logout_state,"offline_request_id","");
 const cJSON*ticket=get(c->auth,"logout_ticket");const char*operation=text(ticket,"operation_id");int found=0;
 char binding[80];logout_binding(ticket,binding);if(binding[0]){cJSON_DeleteItemFromObjectCaseSensitive(c->logout_state,"binding_key");cJSON_AddStringToObject(c->logout_state,"binding_key",binding);if(c->logout_bindings&&!get(c->logout_bindings,binding))cJSON_AddBoolToObject(c->logout_bindings,binding,1);}
 if(queue_release&&operation){for(cJSON*p=pending_logouts(c)->child;p;p=p->next)if(text(p,"operation_id")&&!strcmp(operation,text(p,"operation_id")))found=1;if(!found)cJSON_AddItemToArray(pending_logouts(c),cJSON_Duplicate(ticket,1));}
 int ok=save_logout_state(c);invalidate(c);cJSON_Delete(c->auth);c->auth=NULL;c->auth_dirty=0;
 /* Keep the immutable configuration graph stable for concurrent local checks. */
 const char*private_names[]={"license_key","identity_token"};for(int i=0;i<2;i++){char*secret=(char*)text(c->cfg,private_names[i]);if(secret){size_t count=strlen(secret);volatile unsigned char*p=(volatile unsigned char*)secret;while(count--)*p++=0;}}return ok;
}
static int deny_runtime(ln_client*c){return deny_runtime_mode(c,1);}
static int logout_binding_blocked(ln_client*c,const cJSON*result){
 const char*connection=text(c->cfg,"launch_connection_id");char binding[80];const cJSON*ticket=get(result,"logout_ticket");logout_binding(ticket,binding);
 if(!connection||!connection[0]||!cJSON_IsTrue(get(c->logout_bindings,binding)))return 0;
 if(c->auth){cJSON_DeleteItemFromObjectCaseSensitive(c->auth,"logout_ticket");cJSON_AddItemToObject(c->auth,"logout_ticket",cJSON_Duplicate(ticket,1));}deny_runtime(c);strcpy(c->error,"LAUNCH_LOGIN_REQUIRED");return 1;
}
static int retry_logouts_unlocked(ln_client*c){
 while(has_pending_logouts(c)){
  cJSON*ticket=cJSON_GetArrayItem(pending_logouts(c),0);char*body=cJSON_PrintUnformatted(ticket);const char*base=text(c->cfg,"base_url");char*url=malloc(strlen(base)+32);if(!body||!url){free(body);free(url);return 0;}sprintf(url,"%s/api/v1/sessions/logout",base);
  CURL*curl=curl_easy_init();if(!curl){free(body);free(url);return 0;}struct buffer b={{0},0,0};struct curl_slist*headers=curl_slist_append(NULL,"Content-Type: application/json");
  curl_easy_setopt(curl,CURLOPT_URL,url);curl_easy_setopt(curl,CURLOPT_POSTFIELDS,body);curl_easy_setopt(curl,CURLOPT_HTTPHEADER,headers);curl_easy_setopt(curl,CURLOPT_WRITEFUNCTION,receive);curl_easy_setopt(curl,CURLOPT_WRITEDATA,&b);curl_easy_setopt(curl,CURLOPT_TIMEOUT,5L);curl_easy_setopt(curl,CURLOPT_CONNECTTIMEOUT,5L);curl_easy_setopt(curl,CURLOPT_NOSIGNAL,1L);curl_easy_setopt(curl,CURLOPT_FOLLOWLOCATION,0L);curl_easy_setopt(curl,CURLOPT_SSL_VERIFYPEER,1L);curl_easy_setopt(curl,CURLOPT_SSL_VERIFYHOST,2L);
  const char*proxy=text(c->cfg,"proxy_url"),*ca=getenv("LICENTIVO_CA_BUNDLE");if(proxy&&proxy[0])curl_easy_setopt(curl,CURLOPT_PROXY,proxy);if(ca&&ca[0])curl_easy_setopt(curl,CURLOPT_CAINFO,ca);
  CURLcode error=curl_easy_perform(curl);long status=0;curl_easy_getinfo(curl,CURLINFO_RESPONSE_CODE,&status);curl_easy_cleanup(curl);curl_slist_free_all(headers);free(body);free(url);
  cJSON*envelope=error==CURLE_OK&&!b.oversized?ln_json(b.data):NULL;const cJSON*ack=get(envelope,"data");const char*state=text(ack,"status"),*operation=text(ack,"operation_id"),*activation=text(ack,"activation_id");int64_t generation=0,expected=0;
  int ok=status==200&&state&&(!strcmp(state,"succeeded")||!strcmp(state,"stale"))&&operation&&activation&&text(ticket,"operation_id")&&text(ticket,"activation_id")&&!strcmp(operation,text(ticket,"operation_id"))&&!strcmp(activation,text(ticket,"activation_id"))&&num(ack,"expected_generation",&generation)&&num(ticket,"expected_generation",&expected)&&generation==expected;cJSON_Delete(envelope);if(!ok)return 0;
  char completed[128];snprintf(completed,sizeof(completed),"%s",logout_text(ticket,"operation_id"));cJSON_DeleteItemFromArray(pending_logouts(c),0);if(!acknowledge_logout(c,completed)||!save_logout_state(c))return 0;
 }
 return 1;
}
int ln_retry_pending_logouts(ln_client*c){if(!c)return 0;lock(c);int ok=retry_logouts_unlocked(c);unlock(c);return ok;}
int ln_logout_pending(ln_client*c){if(!c)return 0;lock(c);int pending=has_pending_logouts(c);unlock(c);return pending;}
int ln_logout(ln_client*c){
 if(!c)return 0;
 set_denied(c);lock(c);int ok=deny_runtime(c);retry_logouts_unlocked(c);int pending=has_pending_logouts(c);strcpy(c->error,ok?"DEVICE_RELEASED":"LOCAL_CREDENTIAL_CLEANUP_FAILED");publish(c,0);unlock(c);notify(c);if(pending)ln_start_heartbeat(c);return ok;
}
