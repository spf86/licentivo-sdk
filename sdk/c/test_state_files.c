#include "licentivo.c"

#ifdef _WIN32
static DWORD WINAPI unlock_later(LPVOID handle){Sleep(60);CloseHandle((HANDLE)handle);return 0;}
static int contents(const char*path,const char*expected){char data[32]={0};FILE*f=fopen(path,"rb");if(!f)return 0;size_t n=fread(data,1,sizeof(data)-1,f);fclose(f);return n==strlen(expected)&&!strcmp(data,expected);}
static int put(const char*path,const char*value){FILE*f=fopen(path,"wb");if(!f)return 0;int ok=fputs(value,f)>=0;return fclose(f)==0&&ok;}
static HANDLE hold(const char*path){wchar_t*p=state_windows_path(path);HANDLE h=p?CreateFileW(p,GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE,NULL,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,NULL):INVALID_HANDLE_VALUE;free(p);return h;}
int main(void){
 char from[128],to[128];snprintf(from,sizeof(from),"licentivo-state-%lu-from.tmp",(unsigned long)GetCurrentProcessId());snprintf(to,sizeof(to),"licentivo-state-%lu-to.tmp",(unsigned long)GetCurrentProcessId());
 if(!put(from,"new")||!put(to,"old"))return 1;
 HANDLE locked=hold(to);if(locked==INVALID_HANDLE_VALUE)return 2;
 HANDLE worker=CreateThread(NULL,0,unlock_later,locked,0,NULL);if(!worker){CloseHandle(locked);return 3;}
 int moved=state_windows_move(from,to);WaitForSingleObject(worker,INFINITE);CloseHandle(worker);
 if(!moved||!contents(to,"new")){fprintf(stderr,"Transient journal replacement failed: moved=%d error=%lu\n",moved,(unsigned long)GetLastError());remove(from);remove(to);return 4;}
 if(!put(from,"next"))return 5;locked=hold(to);if(locked==INVALID_HANDLE_VALUE)return 6;
 ULONGLONG start=GetTickCount64();moved=state_windows_move(from,to);ULONGLONG elapsed=GetTickCount64()-start;CloseHandle(locked);
 int ok=!moved&&elapsed<1500&&contents(to,"new")&&contents(from,"next");remove(from);remove(to);
 if(!ok)return 7;
 puts("Windows state journals: transient sharing recovered; persistent sharing fails safely with a bounded wait.");return 0;
}
#else
int main(void){puts("Windows state sharing regression applies to Windows only.");return 0;}
#endif
