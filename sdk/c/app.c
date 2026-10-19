/* A customer application's activation screen and protected report operation. */
#include "licentivo.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/* BEGIN LICENTIVO PRODUCT */
/* Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys. */
static const char *PRODUCT_ID = "YOUR_PRODUCT_ID";
static const char *TRUSTED_KEYS_JSON = "{\"YOUR_KEY_ID\":\"YOUR_PUBLIC_KEY\"}";
/* END LICENTIVO PRODUCT */

static void line(char *value,size_t capacity){if(!fgets(value,(int)capacity,stdin))value[0]=0;value[strcspn(value,"\r\n")]=0;}
static int write_report(void *context){const char *path=(const char*)context;FILE *file=fopen(path,"w");if(!file)return 0;int ok=fputs("Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n",file)>=0;return fclose(file)==0&&ok;}
int main(void){
 char key[80],choice[16];printf("License key (empty to restore this device): ");line(key,sizeof(key));
 ln_client *client=ln_init_product(PRODUCT_ID,TRUSTED_KEYS_JSON,key);
 if(!client){fputs("Check product ID, public keys and activation input.\n",stderr);return 2;}
 if(!ln_start(client)){fprintf(stderr,"%s\n",ln_last_error(client));ln_destroy(client);return 2;}
 for(;;){
  puts("1 Status  2 Export CSV  3 Pro report  4 Refresh license  5 Release device  0 Exit");line(choice,sizeof(choice));
  if(!choice[0]||!strcmp(choice,"0"))break;
  int ok=1;
  if(!strcmp(choice,"1")){char *status=ln_status(client);puts(status?status:"unavailable");ln_free_string(status);}
  if(!strcmp(choice,"2"))ok=ln_run_feature(client,"export",write_report,"licensed-report.csv");
  if(!strcmp(choice,"3"))ok=ln_run_feature(client,"pro",write_report,"pro-report.csv");
  if(!strcmp(choice,"4"))ok=ln_refresh_online(client);
  if(!strcmp(choice,"5"))ok=ln_deactivate(client);
  if(!ok)fprintf(stderr,"Operation blocked or failed: %s\n",ln_last_error(client));
 }
 ln_destroy(client);return 0;
}
