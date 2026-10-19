// A small customer application. The key comes from its activation UI.
#include "licentivo.hpp"
#include <chrono>
#include <fstream>
#include <iostream>
#include <string>

// BEGIN LICENTIVO PRODUCT
// Copy Product ID and all key ID/public key pairs from Console > Products > Signing public keys.
static const std::string PRODUCT_ID = "YOUR_PRODUCT_ID";
static const std::string TRUSTED_KEYS_JSON = R"({"YOUR_KEY_ID":"YOUR_PUBLIC_KEY"})";
// END LICENTIVO PRODUCT

static void writeReport(const char *path){std::ofstream output(path);output.exceptions(std::ios::badbit|std::ios::failbit);output<<"Item,Quantity,Price\nNotebook,2,12.50\nPen,5,3.00\n";output.close();}
int main(){
 std::string key,choice;std::cout<<"License key (empty to restore this device): ";std::getline(std::cin,key);
 try{
  auto client=licentivo::Client::ForProduct(PRODUCT_ID,TRUSTED_KEYS_JSON,key);client.Start();
  while(std::cout<<"1 Status  2 Export CSV  3 Pro report  4 Refresh license  5 Release device  0 Exit\n",std::getline(std::cin,choice)){
   if(choice=="0"||choice.empty())break;
   try{
    if(choice=="1")std::cout<<client.Status()<<'\n';
    if(choice=="2")client.RunFeature("export",[]{writeReport("licensed-report.csv");});
    if(choice=="3")client.RunFeature("pro",[]{writeReport("pro-report.csv");});
    if(choice=="4")client.RefreshOnline();
    if(choice=="5")client.Deactivate();
   }catch(const std::exception&error){std::cerr<<"Operation blocked or failed: "<<error.what()<<'\n';}
  }
 }catch(const std::exception&error){std::cerr<<error.what()<<'\n';return 2;}
}
