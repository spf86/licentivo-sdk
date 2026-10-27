#include "licentivo.hpp"
#include <iostream>
#include <fstream>
#include <sstream>
#include <chrono>
#include <thread>
#include "../c/vendor/cJSON.h"
static std::string readText(const std::string&path){std::ifstream f(path);if(!f)throw std::runtime_error("file missing");std::stringstream s;s<<f.rdbuf();return s.str();}
static void usedIs(const std::string&s,double used,double over){cJSON*j=cJSON_Parse(s.c_str());const cJSON*u=cJSON_GetObjectItemCaseSensitive(j,"used"),*o=cJSON_GetObjectItemCaseSensitive(j,"overage");bool ok=cJSON_IsNumber(u)&&u->valuedouble==used&&cJSON_IsNumber(o)&&o->valuedouble==over;cJSON_Delete(j);if(!ok)throw std::runtime_error("invalid consumption receipt");}
int main(int argc, char **argv) {
    try {
        const std::string path = argc > 2 && std::string(argv[1]) == "--config" ? argv[2] : ".local/protocol-fixture.json";
        bool cacheTest = false;
        for (int i = 1; i < argc; i++) if (std::string(argv[i]) == "--cache-test") cacheTest = true;
        {
            licentivo::Client client(path);
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--offline-lifecycle-test"){
                const auto phase=readText(path+".phase");
                auto importFile=[&](bool fresh=false){const std::string prefix=fresh?".fresh":"";client.ImportOffline(readText(path+prefix+".request.json"),readText(path+prefix+".response.json"));};
                auto exportFile=[&](const std::string&suffix,const std::string&operation){std::ofstream file(path+suffix);file<<client.OfflineRequest(operation);if(!file)throw std::runtime_error("offline export write failed");};
                auto reject=[](const std::function<void()>&action){try{action();}catch(const std::exception&){return;}throw std::runtime_error("operation unexpectedly restored access");};
                auto allowed=[&](){cJSON*state=cJSON_Parse(client.Status().c_str());const cJSON*name=cJSON_GetObjectItemCaseSensitive(state,"state");bool ok=client.CheckFeature("export")&&cJSON_IsString(name)&&std::string(name->valuestring)=="offline_valid";cJSON_Delete(state);if(!ok)throw std::runtime_error("offline import did not publish offline access");};
                if(phase=="export")exportFile(".request.json","activate");
                else if(phase=="import"||phase=="retry"){importFile();if(phase=="retry")importFile();allowed();}
                else if(phase=="fail_import"){reject([&](){importFile();});if(client.CheckFeature("export"))throw std::runtime_error("failed import granted protected access");}
                else if(phase=="release"){importFile();exportFile(".release.json","deactivate");if(client.CheckFeature("export"))throw std::runtime_error("offline release retained access");}
                else if(phase=="signedout"){reject([&](){client.Activate();});reject([&](){client.OfflineRequest();});reject([&](){importFile();});if(client.CheckFeature("export"))throw std::runtime_error("offline release did not survive restart");}
                else if(phase=="logout"){importFile();client.Logout();if(client.LogoutPending()||client.CheckFeature("export"))throw std::runtime_error("logout did not complete");}
                else if(phase=="old_export"||phase=="renew_request"){
                    if(phase=="old_export"){reject([&](){importFile();});if(client.CheckFeature("export"))throw std::runtime_error("old approval restored logged-out access");}
                    exportFile(".fresh.request.json","activate");
                    if(phase=="renew_request"){reject([&](){importFile();});if(client.CheckFeature("export"))throw std::runtime_error("old approval replaced a newer request");}
                }else if(phase=="fresh_import"){importFile(true);allowed();reject([&](){importFile();});allowed();importFile(true);allowed();}
                else throw std::runtime_error("unknown offline lifecycle phase");
                std::cout<<"C++: offline lifecycle phase passed "<<phase<<std::endl;return 0;
            }
            bool meteringRecover=false;for(int i=1;i<argc;i++)if(std::string(argv[i])=="--metering-recover-test")meteringRecover=true;
            bool meteringDenied=false;for(int i=1;i<argc;i++)if(std::string(argv[i])=="--expect-denied")meteringDenied=true;
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--metering-boundary-test"){
                client.Activate();int calls=0;auto action=[&calls](){calls++;};
                if(meteringRecover)for(int j=0;j<2;j++)client.ResolveMeteredFeature("boundary-job",!meteringDenied);
                bool failed=false;try{client.RunMeteredFeature("export",1,"boundary-job",action);}catch(const std::exception&){failed=true;}
                if(failed!=meteringDenied||calls!=(meteringRecover||meteringDenied?0:1))throw std::runtime_error("SDK executed outside authorization or repeated business");
                if(meteringRecover&&meteringDenied){client.RunMeteredFeature("export",1,"replacement-job",action);if(calls!=1)throw std::runtime_error("cancellation did not release quota");}
                std::cout<<"C++: execution boundary regression passed"<<std::endl;return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--metering-expired-cancel-test"||std::string(argv[i])=="--metering-expired-commit-test"){
                bool cancel=std::string(argv[i])=="--metering-expired-cancel-test";int calls=0;auto action=[&calls](){calls++;};
                auto expectFailure=[](const std::function<void()>&callback){try{callback();}catch(const std::exception&){return;}throw std::runtime_error("expected operation failure");};
                if(!meteringRecover){
                    client.Activate();
                    expectFailure([&](){client.RunMeteredFeature("export",1,"expired-job",[&](){calls++;if(cancel)throw std::runtime_error("expected business failure");});});
                    if(calls!=1)throw std::runtime_error("business outcome was lost");
                }else if(cancel){
                    for(int j=0;j<2;j++)client.ResolveMeteredFeature("expired-job",false);
                    expectFailure([&](){client.ResolveMeteredFeature("expired-job",true);});
                    expectFailure([&](){client.RunMeteredFeature("export",1,"expired-job",action);});
                    if(calls!=0)throw std::runtime_error("canceled business repeated");
                    client.RunMeteredFeature("export",1,"replacement-job",action);
                    if(calls!=1)throw std::runtime_error("expired cancellation did not release quota");
                }else{
                    expectFailure([&](){client.RunMeteredFeature("export",1,"expired-job",action);});
                    expectFailure([&](){client.ResolveMeteredFeature("expired-job",true);});
                    expectFailure([&](){client.ResolveMeteredFeature("expired-job",false);});
                    if(calls!=0)throw std::runtime_error("completed business repeated");
                }
                std::cout<<"C++: expired task regression passed"<<std::endl;return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i]).rfind("--metering-",0)==0){
                client.Activate();int calls=0;auto action=[&calls](){calls++;};
                if(std::string(argv[i])=="--metering-jobs-test"){
                    for(const auto&job:{"ExportJob","exportjob","ExportJob"})client.RunMeteredFeature("export",1,job,action);
                    if(calls!=2)throw std::runtime_error("distinct jobs were skipped or repeated");
                }else if(std::string(argv[i])=="--metering-features-test"){
                    for(const auto&feature:{"export","Export"})usedIs(client.Consume(feature,1,std::string(feature)+"-job"),1,0);
                }else{
                    client.Reserve("export",1,"pending-job");bool failed=false;
                    try{client.RunMeteredFeature("export",1,"pending-job",action);}catch(const std::exception&){failed=true;}
                    if(!failed||calls!=0)throw std::runtime_error("pending job bypassed current authorization");
                }
                std::cout<<"C++: metering regression passed"<<std::endl;return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--logout-wait"){
                if(!client.CheckFeature("export"))throw std::runtime_error("missing initial authorization");{std::ofstream ready(path+".ready");ready<<"ready";if(!ready)throw std::runtime_error("write failed");}
                auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(30);while(!std::ifstream(path+".continue")){if(std::chrono::steady_clock::now()>deadline)throw std::runtime_error("logout wait timed out");std::this_thread::sleep_for(std::chrono::milliseconds(10));}
                client.Logout();if(client.CheckFeature("export"))throw std::runtime_error("logout failed");return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--logout-retry"){client.RetryPendingLogouts();if(client.CheckFeature("export"))throw std::runtime_error("logout restored access");std::cout<<"C++: pending logout completed"<<std::endl;return 0;}
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--improvements-test"){
                client.Start();auto read=[](const std::string&v){return cJSON_Parse(v.c_str());};auto check=[](const cJSON*j,const char*status,double used){const cJSON*s=cJSON_GetObjectItemCaseSensitive(j,"status"),*c=cJSON_GetObjectItemCaseSensitive(j,"consumption"),*u=cJSON_GetObjectItemCaseSensitive(c,"used");if(!cJSON_IsString(s)||std::string(s->valuestring)!=status||!cJSON_IsNumber(u)||u->valuedouble!=used)throw std::runtime_error("invalid reservation receipt");};
                cJSON*one=read(client.Reserve("export",1,"cancel-job"));check(one,"pending",0);std::string id=cJSON_GetObjectItemCaseSensitive(one,"reservation_id")->valuestring;cJSON_Delete(one);one=read(client.Cancel(id,"cancel-job"));check(one,"canceled",0);cJSON_Delete(one);
                one=read(client.Reserve("export",1,"commit-job"));id=cJSON_GetObjectItemCaseSensitive(one,"reservation_id")->valuestring;cJSON_Delete(one);for(int k=0;k<2;k++){one=read(client.Commit(id,"commit-job"));check(one,"committed",1);cJSON_Delete(one);}one=read(client.Reserve("export",1,"commit-job"));check(one,"committed",1);cJSON_Delete(one);
                bool failed=false;try{client.Consume("export",1000,"quota-job");}catch(const licentivo::APIError&e){failed=std::string(e.details.code)=="FEATURE_QUOTA_EXCEEDED"&&e.details.request_id[0]&&!e.details.retryable;}if(!failed||!client.CheckFeature("export"))throw std::runtime_error("typed quota status missing");licentivo::Client restored(path,"");restored.Start();restored.Validate();if(!restored.CheckFeature("export"))throw std::runtime_error("normal credential not retained");restored.Deactivate();if(restored.CheckFeature("export"))throw std::runtime_error("release grants access");std::cout<<"C++: credential / reservation / typed status OK"<<std::endl;return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--startup-test"){
                auto start=std::chrono::steady_clock::now();client.Start();
                if(std::chrono::steady_clock::now()-start>std::chrono::milliseconds(300))throw std::runtime_error("cached startup blocked");
                start=std::chrono::steady_clock::now();
                if(!client.CheckFeature("export")||std::chrono::steady_clock::now()-start>std::chrono::milliseconds(300))throw std::runtime_error("local check blocked");
                bool refreshed=false;const auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(5);
                while(!refreshed&&std::chrono::steady_clock::now()<deadline){
                    cJSON*status=ln_json(client.Status().c_str());const cJSON*state=cJSON_GetObjectItemCaseSensitive(status,"state");
                    refreshed=cJSON_IsString(state)&&std::string(state->valuestring)=="active";cJSON_Delete(status);
                    if(!refreshed)std::this_thread::sleep_for(std::chrono::milliseconds(10));
                }
                if(!refreshed)throw std::runtime_error("background refresh did not finish");
                std::cout<<"C++: cached startup and local feature checks stay responsive"<<std::endl;return 0;
            }
            bool advanced=false,offlineExport=false,offlineImport=false,floating=false;
            for(int i=1;i<argc;i++){std::string flag(argv[i]);if(flag=="--advanced-test")advanced=true;if(flag=="--offline-export")offlineExport=true;if(flag=="--offline-import")offlineImport=true;if(flag=="--floating-test")floating=true;}
            if(offlineExport){std::ofstream f(path+".request.json");f<<client.OfflineRequest();if(!f)throw std::runtime_error("write failed");std::cout<<"C++: offline request exported"<<std::endl;return 0;}
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--transfer-test"||std::string(argv[i])=="--transfer-import"){
                if(std::string(argv[i])=="--transfer-import")client.ImportOffline(readText(path+".request.json"),readText(path+".response.json"));else client.Start();
                client.Validate();client.Heartbeat();if(!client.CheckFeature("export"))throw std::runtime_error("feature denied");std::cout<<"C++: transferred device credential OK"<<std::endl;return 0;
            }
            if(floating){client.Start();licentivo::Client other(path);if(other.CheckFeature("export"))throw std::runtime_error("session cache reused");other.Start();client.StopHeartbeat();other.Heartbeat();if(!other.CheckFeature("export"))throw std::runtime_error("new session denied");std::cout<<"C++: floating seat release OK"<<std::endl;return 0;}
            if(advanced||offlineImport){
                if(offlineImport)client.ImportOffline(readText(path+".request.json"),readText(path+".response.json"));else client.Start();
                if(!client.CheckFeature("export"))throw std::runtime_error("feature denied");usedIs(client.Consume("export",1,"business-job-1"),1,0);usedIs(client.Consume("export",1,"business-job-1"),1,0);usedIs(client.Consume("export",3,"business-job-2"),4,1);
                bool failed=false;try{client.Consume("export",1,"business-job-3");}catch(const std::exception&e){failed=std::string(e.what()).find("FEATURE_QUOTA_EXCEEDED")!=std::string::npos;}
                if(!failed||!client.CheckFeature("export"))throw std::runtime_error("quota denial failed");client.Deactivate();std::cout<<"C++: offline/metered feature checks OK"<<std::endl;return 0;
            }
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--refresh-test"){
                bool failed=false;try{client.RefreshOnline();}catch(const std::exception&){failed=true;}
                if(!failed||!client.CheckFeature("export"))throw std::runtime_error("refresh failure did not retain offline access");
                client.RefreshOnline();if(!client.CheckFeature("updated")||client.CheckFeature("export"))throw std::runtime_error("online refresh did not replace cached policy");
                failed=false;try{client.RefreshOnline();}catch(const std::exception&){failed=true;}
                if(!failed||client.CheckFeature("updated"))throw std::runtime_error("refresh denial did not clear cache");
                std::cout<<"C++: online refresh OK"<<std::endl;return 0;
            }
            bool policyTest=false,deny=false;for(int i=1;i<argc;i++){if(std::string(argv[i])=="--policy-test")policyTest=true;if(std::string(argv[i])=="--expect-denied")deny=true;}
            if(policyTest){if(client.CheckFeature("export")==deny)throw std::runtime_error("unexpected cache device/policy access");if(!deny){client.Activate();client.Validate();client.Heartbeat();}std::cout<<"C++: policy cache OK"<<std::endl;return 0;}
            client.Activate();
            if (!cacheTest) {
                client.Validate(); client.Heartbeat();
                std::cout << "C++: activated, validated, heartbeat OK; export enabled " << client.CheckFeature("export") << std::endl;
                client.Deactivate();
                std::cout << "C++: deactivated" << std::endl;
                return 0;
            }
        }
        {
            licentivo::Client restored(path);
            restored.Validate();
            if (!restored.CheckFeature("export")) throw std::runtime_error("cache not restored during server outage");
            bool rejected = false;
            try { restored.Validate(); } catch (const std::runtime_error &) { rejected = true; }
            if (!rejected || restored.CheckFeature("export")) throw std::runtime_error("rejection did not invalidate cache");
        }
        {
            licentivo::Client reopened(path);
            if (reopened.CheckFeature("export")) throw std::runtime_error("rejected cache survived restart");
        }
        std::cout << "C++: cache restored on 503; 403 invalidated access and persisted cache" << std::endl;
    } catch (const std::exception &e) {
        std::cerr << "C++: " << e.what() << std::endl;
        return 1;
    }
}
