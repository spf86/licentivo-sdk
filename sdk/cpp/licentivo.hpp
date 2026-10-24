#pragma once
#include "../c/licentivo.h"
#include <stdexcept>
#include <string>
#include <functional>
#include <exception>
namespace licentivo {
class APIError:public std::runtime_error {public:ln_error_info details;explicit APIError(const ln_error_info&info):std::runtime_error(info.code),details(info){}};
// The C++ SDK shares the tested C signing/transport core and provides RAII ownership.
class Client {
 ln_client* client;
 std::function<void(const std::string&)> listener;
 void require(int ok) {if(!ok){ln_error_info info{};ln_get_error_info(client,&info);if(!info.code[0])throw std::runtime_error(ln_last_error(client));throw APIError(info);}}
 std::string result(char*value){if(!value)require(0);std::string out(value);ln_free_string(value);return out;}
explicit Client(ln_client*handle):client(handle){if(!client)throw std::invalid_argument("invalid product ID, public keys or license key");}
public:
 static Client FromConfig(const std::string&configJSON){return Client(ln_init(configJSON.c_str()));}
 static Client ForProduct(const std::string&productID,const std::string&trustedKeysJSON,const std::string&licenseKey){return Client(ln_init_product(productID.c_str(),trustedKeysJSON.c_str(),licenseKey.c_str()));}
 std::string Status(){return result(ln_status(client));}
 // Local entitlement decision. Metered operations still require Reserve.
 std::string FeatureStatus(const std::string&feature){return result(ln_feature_status(client,feature.c_str()));}
 void RunFeature(const std::string&feature,const std::function<void()>&action){if(!action)throw std::invalid_argument("business action is required");if(!CheckFeature(feature))throw std::runtime_error("feature denied: "+FeatureStatus(feature));action();}
 void RunMeteredFeature(const std::string&feature,int64_t quantity,const std::string&operationID,const std::function<void()>&action){
  if(!action)throw std::invalid_argument("business action is required");
  struct Context{const std::function<void()>*action;std::exception_ptr error;} context{&action,{}};
  int ok=ln_run_metered_feature(client,feature.c_str(),quantity,operationID.c_str(),[](void*raw){auto*c=static_cast<Context*>(raw);try{(*c->action)();return 1;}catch(...){c->error=std::current_exception();return 0;}},&context);
  if(context.error)std::rethrow_exception(context.error);require(ok);
 }
 void ResolveMeteredFeature(const std::string&operationID,bool businessSucceeded){require(ln_resolve_metered_feature(client,operationID.c_str(),businessSucceeded));}
 void OnStatusChanged(std::function<void(const std::string&)> callback){listener=std::move(callback);ln_on_status_changed(client,[](const char*status,void*context){auto*self=static_cast<Client*>(context);if(self->listener)self->listener(status);},this);}
 std::string Reserve(const std::string&feature,int64_t quantity,const std::string&operationID){return result(ln_reserve(client,feature.c_str(),quantity,operationID.c_str()));}
 std::string Commit(const std::string&id,const std::string&operationID){return result(ln_commit(client,id.c_str(),operationID.c_str()));}
 std::string Cancel(const std::string&id,const std::string&operationID){return result(ln_cancel(client,id.c_str(),operationID.c_str()));}
 explicit Client(const std::string&configPath):client(ln_init_file(configPath.c_str())){if(!client)throw std::invalid_argument("invalid SDK configuration");}
 // Shared product settings; the application supplies the customer's license key.
 Client(const std::string&configPath,const std::string&licenseKey):client(ln_init_file_with_license(configPath.c_str(),licenseKey.c_str())){if(!client)throw std::invalid_argument("invalid SDK configuration or license key");}
 ~Client(){ln_destroy(client);}
 Client(const Client&)=delete;Client&operator=(const Client&)=delete;
 void Start(){require(ln_start(client));}
 std::string Consume(const std::string&feature,int64_t quantity,const std::string&operationID){char*value=ln_consume(client,feature.c_str(),quantity,operationID.c_str());if(!value)require(0);std::string out(value);ln_free_string(value);return out;}
 std::string OfflineRequest(const std::string&operation="activate"){char*value=ln_offline_request(client,operation.c_str());if(!value)throw std::runtime_error("offline request failed");std::string out(value);ln_free_string(value);return out;}
 void ImportOffline(const std::string&request,const std::string&response){require(ln_import_offline(client,request.c_str(),response.c_str()));}
 void Activate(){require(ln_activate(client));}void Validate(){require(ln_validate(client));}void Heartbeat(){require(ln_heartbeat(client));}void RefreshOnline(){require(ln_refresh_online(client));}
 bool CheckFeature(const std::string&feature){return ln_check_feature(client,feature.c_str())!=0;}
 void StartHeartbeat(){require(ln_start_heartbeat(client));}void StopHeartbeat(){ln_stop_heartbeat(client);}void Deactivate(){require(ln_deactivate(client));}
 void Logout(){require(ln_logout(client));}void RetryPendingLogouts(){require(ln_retry_pending_logouts(client));}bool LogoutPending(){return ln_logout_pending(client)!=0;}
};
}
