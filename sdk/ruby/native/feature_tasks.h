/* Included by licentivo.c only. Private task IDs/outcome, never business data. */
static int task_error(ln_client*c,const char*code){lock(c);snprintf(c->error,sizeof(c->error),"%s",code);c->http_status=0;c->retryable=0;c->request_id[0]=0;view_lock(c);int online=c->view_online;view_unlock(c);publish(c,online);unlock(c);return 0;}
static int task_enter(ln_client*c){view_lock(c);int busy=c->task_busy;if(!busy)c->task_busy=1;view_unlock(c);return !busy;}
static void task_leave(ln_client*c){view_lock(c);c->task_busy=0;view_unlock(c);}
static char*task_path(ln_client*c,const char*id){
 if(!id||!id[0]||strlen(id)>80)return NULL;
 for(const unsigned char*p=(const unsigned char*)id;*p;p++)if(*p<33||*p>126)return NULL;
 char source[65],digest[65],input[256];lock(c);
 const char*key=text(c->cfg,"license_key");int ok=key&&key[0]?hash(key,source):text(c->auth,"source_hash")!=NULL;
 if(ok&&(!key||!key[0]))snprintf(source,sizeof(source),"%s",text(c->auth,"source_hash"));unlock(c);
 if(!ok)return NULL;snprintf(input,sizeof(input),"%s\n%s",source,id);if(!hash(input,digest))return NULL;
 const char*cache=text(c->cfg,"cache_path");if(!cache)return NULL;
 char*path=malloc(strlen(cache)+78);if(path)sprintf(path,"%s.task-%s.json",cache,digest);return path;
}
static cJSON*task_read(const char*path,const char*id){
 struct stat info;if(stat(path,&info)==0){if(info.st_size>4096)return NULL;char*raw=readfile(path,4096);cJSON*task=raw?ln_json(raw):NULL;free(raw);int64_t version;
  int64_t quantity;
  if(!task||!num(task,"version",&version)||version!=1||!text(task,"operation_id")||strcmp(text(task,"operation_id"),id)||!text(task,"reservation_id")||!text(task,"reservation_id")[0]||!text(task,"state")||!text(task,"feature")||!text(task,"feature")[0]||!num(task,"quantity",&quantity)||quantity<=0){cJSON_Delete(task);return NULL;}return task;
 }
 if(errno!=ENOENT)return NULL;
 cJSON*task=cJSON_CreateObject();cJSON_AddNumberToObject(task,"version",1);cJSON_AddStringToObject(task,"operation_id",id);cJSON_AddStringToObject(task,"reservation_id","");cJSON_AddStringToObject(task,"state","");return task;
}
static void task_state(cJSON*task,const char*state){cJSON_ReplaceItemInObjectCaseSensitive(task,"state",cJSON_CreateString(state));}
static int task_finish(ln_client*c,const char*path,cJSON*task,int succeeded){
 char*raw=succeeded?ln_commit(c,text(task,"reservation_id"),text(task,"operation_id")):ln_cancel(c,text(task,"reservation_id"),text(task,"operation_id"));
 cJSON*receipt=raw?ln_json(raw):NULL;ln_free_string(raw);if(!receipt)return 0;
 const char*state=text(receipt,"status"),*expected=succeeded?"committed":"canceled";
 int ok=state&&!strcmp(state,expected);cJSON_Delete(receipt);
 if(!ok)return task_error(c,"RESERVATION_CLOSED");task_state(task,expected);
 return write_state(path,task)?1:task_error(c,"TASK_JOURNAL_ERROR");
}
int ln_run_metered_feature(ln_client*c,const char*feature,int64_t quantity,const char*id,ln_feature_action action,void*context){
 if(!c||!feature||!action||quantity<=0)return c?task_error(c,"TASK_INPUT_INVALID"):0;
 if(!task_enter(c))return task_error(c,"TASK_BUSY");
 int ok=0;char*path=task_path(c,id);cJSON*task=path?task_read(path,id):NULL;
 if(!task){task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 const char*state=text(task,"state");
 if(state[0]){int64_t old_quantity;if(!text(task,"feature")||strcmp(text(task,"feature"),feature)||!num(task,"quantity",&old_quantity)||old_quantity!=quantity){task_error(c,"OPERATION_ID_CONFLICT");goto done;}}
 if(!state[0]){cJSON_AddStringToObject(task,"feature",feature);cJSON_AddNumberToObject(task,"quantity",(double)quantity);}
 if(!strcmp(state,"committed")){ok=1;goto done;}
 if(!strcmp(state,"commit_pending")){ok=task_finish(c,path,task,1);goto done;}
 if(!strcmp(state,"cancel_pending")){if(!task_finish(c,path,task,0))goto done;task_error(c,"OPERATION_CANCELED");goto done;}
 if(!strcmp(state,"canceled")){task_error(c,"OPERATION_CANCELED");goto done;}
 if(!strcmp(state,"business_running")){task_error(c,"BUSINESS_OUTCOME_UNKNOWN");goto done;}
 if(state[0]){task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 if(!ln_check_feature(c,feature)){task_error(c,"FEATURE_NOT_ENTITLED");goto done;}
 char*raw=ln_reserve(c,feature,quantity,id);cJSON*hold=raw?ln_json(raw):NULL;ln_free_string(raw);
 if(!hold)goto done;
 const char*held=text(hold,"status");
 if(held&&!strcmp(held,"committed")){cJSON_ReplaceItemInObjectCaseSensitive(task,"reservation_id",cJSON_CreateString(text(hold,"reservation_id")));task_state(task,"committed");cJSON_Delete(hold);ok=write_state(path,task)?1:task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 if(!held||strcmp(held,"pending")||!text(hold,"reservation_id")){cJSON_Delete(hold);task_error(c,"RESERVATION_CLOSED");goto done;}
 cJSON_ReplaceItemInObjectCaseSensitive(task,"reservation_id",cJSON_CreateString(text(hold,"reservation_id")));cJSON_Delete(hold);task_state(task,"business_running");
 if(!write_state(path,task)){raw=ln_cancel(c,text(task,"reservation_id"),id);ln_free_string(raw);task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 if(!action(context)){
  task_state(task,"cancel_pending");if(write_state(path,task))task_finish(c,path,task,0);task_error(c,"BUSINESS_ACTION_FAILED");goto done;
 }
 task_state(task,"commit_pending");if(!write_state(path,task)){task_error(c,"BUSINESS_COMPLETED_JOURNAL_ERROR");goto done;}
 ok=task_finish(c,path,task,1);
done:
 cJSON_Delete(task);free(path);task_leave(c);return ok;
}
int ln_resolve_metered_feature(ln_client*c,const char*id,int succeeded){
 if(!c)return 0;if(!task_enter(c))return task_error(c,"TASK_BUSY");
 int ok=0;char*path=task_path(c,id);cJSON*task=path?task_read(path,id):NULL;
 if(!task){task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 const char*state=text(task,"state");if(!strcmp(state,succeeded?"committed":"canceled")){ok=1;goto done;}
 if(!text(task,"reservation_id")[0]||(strcmp(state,"business_running")&&strcmp(state,"commit_pending")&&strcmp(state,"cancel_pending"))||(!succeeded&&!strcmp(state,"commit_pending"))||(succeeded&&!strcmp(state,"cancel_pending"))){task_error(c,"TASK_OUTCOME_CONFLICT");goto done;}
 task_state(task,succeeded?"commit_pending":"cancel_pending");if(!write_state(path,task)){task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 ok=task_finish(c,path,task,succeeded);
done:
 cJSON_Delete(task);free(path);task_leave(c);return ok;
}
