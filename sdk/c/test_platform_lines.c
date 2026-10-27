#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

static const char*input;
static size_t offset,interruption;
static int interrupted,read_error,cleared,hard_error;
static int probe_getc(FILE*stream){
 (void)stream;
 if(offset==interruption&&!interrupted){interrupted=1;read_error=1;errno=hard_error?EIO:EINTR;return EOF;}
 return input[offset]?(unsigned char)input[offset++]:EOF;
}
static int probe_ferror(FILE*stream){(void)stream;return read_error;}
static void probe_clearerr(FILE*stream){(void)stream;read_error=0;cleared++;}
#define LICENTIVO_TEST_PLATFORM_LINES 1
#define fgetc probe_getc
#define ferror probe_ferror
#define clearerr probe_clearerr
#include "licentivo.c"
#undef fgetc
#undef ferror
#undef clearerr

static void reset(const char*text,size_t position,int failure){
 input=text;offset=0;interruption=position;interrupted=read_error=cleared=0;hard_error=failure;
}
int main(void){
 const char*line="  \"IOPlatformUUID\" = \"abc123\"\n";
 char output[128];FILE*stream=stdin;
 for(size_t position=0;position<=strlen(line);position++){
  reset(line,position,0);
  if(!platform_read_line(stream,output,sizeof(output))||strcmp(output,line))return 1;
  if(platform_read_line(stream,output,sizeof(output))!=NULL||!interrupted||cleared!=1)return 2;
 }
 reset("final line without newline",5,0);
 if(!platform_read_line(stream,output,sizeof(output))||strcmp(output,input)||cleared!=1)return 3;
 if(platform_read_line(stream,output,sizeof(output)))return 4;
 reset("abcdef",3,0);
 if(!platform_read_line(stream,output,4)||strcmp(output,"abc"))return 5;
 if(!platform_read_line(stream,output,4)||strcmp(output,"def")||cleared!=1)return 6;
 reset("identity must fail on a real I/O error",5,1);
 if(platform_read_line(stream,output,sizeof(output))||cleared)return 7;
 puts("OS identity lines: interrupted reads preserve bytes; EOF and real errors remain distinct.");return 0;
}
