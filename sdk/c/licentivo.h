#ifndef LICENTIVO_H
#define LICENTIVO_H
#include <stdint.h>
#include "vendor/cJSON.h"
#ifdef __cplusplus
extern "C" {
#endif
typedef struct ln_client ln_client;
typedef struct ln_error_info { int http_status;char code[80];char request_id[128];int retryable;int retry_after_seconds; } ln_error_info;
typedef void (*ln_status_callback)(const char *status_json,void *context);
/* Local reads stay available during a network request. Callback runs after the
   request lock is released; marshal it to your UI thread. Do not destroy the
   client from a callback. Returned status/reservation strings need ln_free_string. */
char *ln_status(ln_client *client);
/* Local feature decision, with FEATURE_NOT_ENTITLED when the grant omits it.
   This checks entitlement only; metered operations still require ln_reserve.
   The caller owns the returned JSON string and must call ln_free_string. */
char *ln_feature_status(ln_client *client,const char *feature);
/* Business callbacks return 1 on success, 0 on failure. Denied callbacks never run. */
typedef int (*ln_feature_action)(void *context);
int ln_run_feature(ln_client *client,const char *feature,ln_feature_action action,void *context);
/* Stable job IDs are required. Pending confirmations are journaled privately.
   Reusing an ID retries confirmation only, never a completed business callback.
   Use a distinct ID per job; separate processes must not execute the same ID.
   The callback must not destroy this client. */
int ln_run_metered_feature(ln_client*,const char *feature,int64_t quantity,const char *operation_id,ln_feature_action action,void *context);
int ln_resolve_metered_feature(ln_client*,const char *operation_id,int business_succeeded);
int ln_get_error_info(ln_client *client,ln_error_info *out);
void ln_on_status_changed(ln_client *client,ln_status_callback callback,void *context);
char *ln_reserve(ln_client*,const char *feature,int64_t quantity,const char *operation_id);
char *ln_commit(ln_client*,const char *reservation_id,const char *operation_id);
char *ln_cancel(ln_client*,const char *reservation_id,const char *operation_id);
/* All runtime operations are serialized. Configuration pins keys and contains a license secret. */
ln_client *ln_init(const char *config_json);
/* Product ID and key-id/public-key map from Console > Products > Signing public keys.
   The service URL defaults to https://www.licentivo.com. Call ln_start afterwards. */
ln_client *ln_init_product(const char *product_id,const char *trusted_keys_json,const char *license_key);
ln_client *ln_init_file(const char *path);
/* Shared product config; the application supplies the customer's license key. */
ln_client *ln_init_file_with_license(const char *path,const char *license_key);
int ln_activate(ln_client *client);
/* Simple lifecycle: open activates and starts policy heartbeats; destroy releases floating seats. */
ln_client *ln_open(const char *config_path);
ln_client *ln_open_with_license(const char *config_path,const char *license_key);
int ln_start(ln_client *client);
/* Returned strings belong to the caller; free them with ln_free_string. */
char *ln_consume(ln_client *client,const char *feature,int64_t quantity,const char *operation_id);
char *ln_offline_request(ln_client *client,const char *operation);
int ln_import_offline(ln_client *client,const char *request_json,const char *response_json);
void ln_free_string(char *value);
int ln_validate(ln_client *client);
int ln_heartbeat(ln_client *client);
int ln_refresh_online(ln_client *client);
int ln_check_feature(ln_client *client,const char *feature);
int ln_deactivate(ln_client *client);
int ln_start_heartbeat(ln_client *client);
void ln_stop_heartbeat(ln_client *client);
void ln_destroy(ln_client *client);
const char *ln_last_error(const ln_client *client);
/* Returns 1 only for a correctly signed, bound, unexpired lease. Useful for cross-language vectors. */
int ln_verify(const cJSON *envelope,const cJSON *pinned_keys,const char *product,const char *device,int64_t now);
/* Caller owns the JSON tree. The parser rejects duplicate fields and trailing data. */
cJSON *ln_json(const char *json);
#ifdef __cplusplus
}
#endif
#endif
