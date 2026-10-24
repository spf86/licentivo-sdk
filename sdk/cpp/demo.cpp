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
            for(int i=1;i<argc;i++)if(std::string(argv[i])=="--startup-test"){auto start=std::chrono::steady_clock::now();client.Start();if(std::chrono::steady_clock::now()-start>std::chrono::milliseconds(300))throw std::runtime_error("cached startup blocked");start=std::chrono::steady_clock::now();if(!client.CheckFeature("export")||std::chrono::steady_clock::now()-start>std::chrono::milliseconds(300))throw std::runtime_error("local check blocked");std::cout<<"C++: cached startup and local feature checks stay responsive"<<std::endl;return 0;}
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
