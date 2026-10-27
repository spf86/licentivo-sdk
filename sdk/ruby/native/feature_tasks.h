/* Included by licentivo.c only. Private task IDs/outcome, never business data. */
static int task_time_digits(const char*value,int offset,int length,int*out){
 int result=0;for(int i=0;i<length;i++){unsigned char ch=(unsigned char)value[offset+i];if(ch<'0'||ch>'9')return 0;result=result*10+ch-'0';}*out=result;return 1;
}
/* Reservation receipts use RFC 3339, including fractional seconds and offsets.
 * Convert the civil date directly so parsing is independent of the host timezone. */
static int task_deadline(const char*value,double*out){
 if(!value)return 0;size_t length=strlen(value);if(length<20||length>35)return 0;
 if(value[4]!='-'||value[7]!='-'||value[10]!='T'||value[13]!=':'||value[16]!=':')return 0;
 int year,month,day,hour,minute,second;
 if(!task_time_digits(value,0,4,&year)||!task_time_digits(value,5,2,&month)||!task_time_digits(value,8,2,&day)||!task_time_digits(value,11,2,&hour)||!task_time_digits(value,14,2,&minute)||!task_time_digits(value,17,2,&second))return 0;
 if(year<1||month<1||month>12||hour>23||minute>59||second>59)return 0;
 const int month_days[]={31,28,31,30,31,30,31,31,30,31,30,31};int leap=year%4==0&&(year%100!=0||year%400==0);
 if(day<1||day>month_days[month-1]+(month==2&&leap))return 0;
 size_t pos=19;double fraction=0;
 if(value[pos]=='.'){
  pos++;int digits=0;double place=.1;
  while(pos<length&&value[pos]>='0'&&value[pos]<='9'){if(++digits>9)return 0;fraction+=(value[pos++]-'0')*place;place*=.1;}
  if(!digits)return 0;
 }
 int offset=0;
 if(pos+1==length&&value[pos]=='Z')pos++;
 else if(pos+6==length&&(value[pos]=='+'||value[pos]=='-')&&value[pos+3]==':'){
  int hours,minutes;if(!task_time_digits(value,(int)pos+1,2,&hours)||!task_time_digits(value,(int)pos+4,2,&minutes)||hours>23||minutes>59)return 0;
  offset=(hours*60+minutes)*60*(value[pos]=='+'?1:-1);pos+=6;
 }else return 0;
 if(pos!=length)return 0;
 int64_t previous=year-1,days=365*previous+previous/4-previous/100+previous/400-719162;
 for(int m=1;m<month;m++)days+=month_days[m-1]+(m==2&&leap);
 *out=(double)((days+day-1)*86400+hour*3600+minute*60+second-offset)+fraction;return 1;
}
static double task_wall_time(void){
#ifdef _WIN32
 FILETIME raw;ULARGE_INTEGER ticks;GetSystemTimeAsFileTime(&raw);ticks.LowPart=raw.dwLowDateTime;ticks.HighPart=raw.dwHighDateTime;
 return (double)(ticks.QuadPart-116444736000000000ULL)/10000000.0;
#else
 struct timespec now;if(clock_gettime(CLOCK_REALTIME,&now)==0)return (double)now.tv_sec+now.tv_nsec/1e9;
 return (double)time(NULL);
#endif
}
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
 int ok=state&&(!strcmp(state,expected)||(!succeeded&&!strcmp(state,"expired")));cJSON_Delete(receipt);
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
 double deadline=0;int valid_deadline=task_deadline(text(hold,"expires_at"),&deadline);
 cJSON_ReplaceItemInObjectCaseSensitive(task,"reservation_id",cJSON_CreateString(text(hold,"reservation_id")));cJSON_Delete(hold);task_state(task,"business_running");
 if(!write_state(path,task)){raw=ln_cancel(c,text(task,"reservation_id"),id);ln_free_string(raw);task_error(c,"TASK_JOURNAL_ERROR");goto done;}
 int allowed=ln_check_feature(c,feature),unexpired=valid_deadline&&deadline>task_wall_time();
 if(!allowed||!unexpired||!action(context)){
  const char*failure=!allowed?"FEATURE_NOT_ENTITLED":!unexpired?"RESERVATION_EXPIRED":"BUSINESS_ACTION_FAILED";
  task_state(task,"cancel_pending");if(write_state(path,task))task_finish(c,path,task,0);task_error(c,failure);goto done;
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
