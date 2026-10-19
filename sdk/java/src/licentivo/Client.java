package licentivo;

import java.io.*;
import java.net.*;
import java.net.http.*;
import java.nio.*;
import java.nio.charset.*;
import java.nio.file.*;
import java.nio.file.attribute.*;
import java.security.*;
import java.security.spec.*;
import java.time.*;
import java.util.*;
import java.util.concurrent.*;

/** Java 17+ SDK. Native JCA Ed25519, no third-party Java dependencies. */
public final class Client implements AutoCloseable {
    private FeatureTasks featureTasks;
    private synchronized FeatureTasks featureTasks() throws Exception {
        if(featureTasks==null)featureTasks=new FeatureTasks(this,cachePath(),hash());
        return featureTasks;
    }
    public void runMeteredFeature(String feature,long quantity,String operationID,FeatureAction action) throws Exception {featureTasks().run(feature,quantity,operationID,action);}
    public void resolveMeteredFeature(String operationID,boolean succeeded) throws Exception {featureTasks().resolve(operationID,succeeded);}
    @FunctionalInterface public interface FeatureAction { void run() throws Exception; }
    /** Non-metered entitlement check; denied operations never invoke the callback. */
    public void runFeature(String feature, FeatureAction action) throws Exception {
        Objects.requireNonNull(action, "business action is required");
        var status = featureStatus(feature);
        if (!Boolean.TRUE.equals(status.get("allowed")))
            throw new IllegalStateException("feature denied: " + status.get("code") + " (" + status.get("state") + "); request_id=" + status.get("request_id"));
        action.run();
    }
    private static final int LIMIT=16384;
    private final Map<String,Object> profile;private final DeviceCredential credential;private String sessionID=UUID.randomUUID().toString();private final Map<String,Object> cfg;private final Map<String,String> keys;private final HttpClient http;
    private volatile View view;private volatile boolean online;private volatile Exception lastError;private volatile long retryAt;private volatile java.util.function.Consumer<Map<String,Object>> statusListener;private Thread refresh;
    private record View(Map<String,Object> state,long wall,long tick){}
    private Map<String,Object> state;private long wall;private long tick=System.nanoTime();private volatile int interval=3600;private volatile boolean stopped;private Thread heartbeat;
    public static final class APIException extends IOException {public final int status;public final String code,requestID;public final boolean retryable;public final int retryAfterSeconds;public APIException(int status,String code){this(status,code,"",0);}public APIException(int status,String code,String requestID,int retryAfterSeconds){super("Licentivo "+code+" ("+status+")");this.status=status;this.code=code;this.requestID=requestID;this.retryable=status==429||status>=500;this.retryAfterSeconds=retryAfterSeconds;}}
    public static Client openProduct(String productID,Map<String,String> trustedKeys,String licenseKey,java.util.function.Consumer<Exception> onError)throws Exception {
        Client client=new Client(Map.of("product_id",productID,"trusted_keys",trustedKeys,"license_key",Objects.requireNonNull(licenseKey,"licenseKey").strip()));
        try{client.start(onError);return client;}catch(Exception error){client.close();throw error;}
    }
    public Client(Map<String,Object> config)throws Exception {
        cfg=new LinkedHashMap<>(config);if(!cfg.containsKey("base_url")||Json.text(cfg,"base_url").isEmpty())cfg.put("base_url","https://www.licentivo.com");cfg.putIfAbsent("license_key","");cfg.put("base_url",Json.text(cfg,"base_url").replaceAll("/$",""));if(!cfg.containsKey("cache_path")||Json.text(cfg,"cache_path").isEmpty()){String root=System.getenv("LOCALAPPDATA");if(root==null)root=System.getenv("XDG_CACHE_HOME");if(root==null)root=Path.of(System.getProperty("user.home"),".cache").toString();String scope=HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest((Json.text(cfg,"base_url")+"\n"+Json.text(cfg,"product_id")).getBytes(StandardCharsets.UTF_8)));cfg.put("cache_path",Path.of(root,"Licentivo",scope,"lease.json").toString());}profile=MachineIdentity.profile(Json.text(cfg,"product_id"));cfg.put("device_id",MachineIdentity.deviceID(Json.text(cfg,"product_id"),profile));URI u=URI.create(Json.text(cfg,"base_url"));String host=u.getHost();
        if(host==null||u.getUserInfo()!=null||u.getQuery()!=null||u.getFragment()!=null||!(u.getPath().isEmpty()||u.getPath().equals("/")))throw new IllegalArgumentException("base_url must be an origin");
        if(!u.getScheme().equals("https")&&!(Boolean.TRUE.equals(cfg.get("allow_http"))&&u.getScheme().equals("http")&&Set.of("localhost","127.0.0.1","[::1]","::1").contains(host)))throw new IllegalArgumentException("HTTPS required");
        if(!Json.text(cfg,"device_id").matches("[0-9a-f]{64}")||!Json.text(cfg,"license_key").isEmpty()&&!Json.text(cfg,"license_key").matches("lv_(lic|tmp)_[A-Za-z0-9_-]{43}"))throw new IllegalArgumentException("invalid device/license");
        keys=new HashMap<>();for(Map.Entry<String,Object> e:Json.object(cfg.get("trusted_keys")).entrySet()){String s=(String)e.getValue();if(decode(s).length!=32)throw new IllegalArgumentException("invalid pinned key");keys.put(e.getKey(),s);}if(keys.isEmpty())throw new IllegalArgumentException("pin a public key");
        credential=new DeviceCredential(cfg);sessionID=credential.session(sessionID);if(Json.text(cfg,"license_key").isEmpty()&&credential.key().isEmpty())throw new IllegalArgumentException("enter a license key or temporary activation code");int seconds=timeout();if(seconds<1||seconds>120)throw new IllegalArgumentException("timeout_seconds must be 1–120");HttpClient.Builder builder=HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(Math.min(5,seconds))).followRedirects(HttpClient.Redirect.NEVER);if(cfg.containsKey("proxy_url")&&!Json.text(cfg,"proxy_url").isEmpty()){URI proxy=URI.create(Json.text(cfg,"proxy_url"));if(!"http".equals(proxy.getScheme())||proxy.getHost()==null)throw new IllegalArgumentException("Java proxy_url must be an HTTP proxy URL");builder.proxy(ProxySelector.of(new InetSocketAddress(proxy.getHost(),proxy.getPort()<0?8080:proxy.getPort())));}http=builder.build();
        try{Path p=cachePath();if(p!=null&&Files.size(p)<=LIMIT){Map<String,Object>s=Json.object(Json.parse(Files.readString(p)));if(Json.number(s,"version")==1&&hash().equals(s.get("license_hash"))&&now()+300>=Json.number(s,"observed_at")){Map<String,Object> c=verify(Json.object(s.get("lease")),keys,Json.text(cfg,"product_id"),Json.text(cfg,"device_id"),now());if(!RuntimePolicy.mode(c).equals("none")&&!Capabilities.floating(c)){Capabilities.context(c,cfg,sessionID);state=s;wall=Json.number(s,"observed_at");interval=RuntimePolicy.heartbeat(c,3600);}}}}catch(Exception ignored){}
        publish(false,null);
    }
    public static String deviceID(String product,String installation)throws Exception {return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(("licentivo-device-v1\n"+product+"\n"+installation.trim().toLowerCase(Locale.ROOT)).getBytes(StandardCharsets.UTF_8)));}
    public static byte[] decode(String s){if(!s.matches("[A-Za-z0-9_-]+"))throw new IllegalArgumentException("invalid base64url");byte[] b=Base64.getUrlDecoder().decode(s);if(!Base64.getUrlEncoder().withoutPadding().encodeToString(b).equals(s))throw new IllegalArgumentException("noncanonical base64url");return b;}
    private static boolean uuid(String s){return s.matches("[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}");}
    public static Map<String,Object> verify(Map<String,Object> envelope,Map<String,String> keys,String product,String device,long now)throws Exception{
        if(!envelope.keySet().equals(Set.of("version","key_id","payload","signature"))||Json.number(envelope,"version")!=1)throw new IllegalArgumentException("invalid envelope");String keyID=Json.text(envelope,"key_id");if(!keys.containsKey(keyID))throw new IllegalArgumentException("unknown pinned key");byte[] raw=decode(Json.text(envelope,"payload")),sig=decode(Json.text(envelope,"signature")),pub=decode(keys.get(keyID));if(pub.length!=32||sig.length!=64||raw.length>LIMIT)throw new IllegalArgumentException("invalid lengths");
        byte[] der=new byte[44];System.arraycopy(HexFormat.of().parseHex("302a300506032b6570032100"),0,der,0,12);System.arraycopy(pub,0,der,12,32);PublicKey key=KeyFactory.getInstance("Ed25519").generatePublic(new X509EncodedKeySpec(der));Signature verifier=Signature.getInstance("Ed25519");verifier.initVerify(key);verifier.update("LicenovaLease/v1\n".getBytes(StandardCharsets.UTF_8));verifier.update(raw);if(!verifier.verify(sig))throw new SignatureException("invalid lease signature");
        String utf8=StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(raw)).toString();Map<String,Object>c=Json.object(Json.parse(utf8));
        if(!RuntimePolicy.fields(c)||Json.number(c,"version")!=1||!keyID.equals(Json.text(c,"key_id"))||!product.equals(Json.text(c,"product_id"))||!device.equals(Json.text(c,"device_id"))||!device.matches("[0-9a-f]{64}"))throw new IllegalArgumentException("lease context mismatch");for(String f:new String[]{"tenant_id","product_id","license_id","activation_id"})if(!uuid(Json.text(c,f)))throw new IllegalArgumentException("invalid identifier");
        long issued=Json.number(c,"issued_at"),expires=Json.number(c,"expires_at"),max=Json.number(c,"max_devices");if(!Set.of("perpetual","timed","trial").contains(Json.text(c,"mode"))||max<1||max>10000||issued<=0||expires<=issued||expires-issued>RuntimePolicy.maxSpan(c)||expires>253402300799L||issued>now+300||now>=expires)throw new IllegalArgumentException("expired lease/clock");
        Object features=c.get("features");if(!(features instanceof List)||((List<?>)features).size()>32)throw new IllegalArgumentException("invalid features");String previous="";for(Object f:(List<?>)features){if(!(f instanceof String)||!((String)f).matches("[A-Za-z0-9_.-]{1,64}")||previous.compareTo((String)f)>=0)throw new IllegalArgumentException("invalid features");previous=(String)f;}Capabilities.verify(c);return c;
    }
    private static long now(){return Instant.now().getEpochSecond();}
    private String hash()throws Exception{return credential.cacheHash();}
    private Path cachePath(){Object p=cfg.get("cache_path");return p instanceof String&&!((String)p).isEmpty()?Path.of((String)p):null;}
    private Map<String,Object>check()throws Exception{if(wall>0&&now()<wall+(System.nanoTime()-tick)/1000000000L-300)throw new IllegalStateException("clock rollback; verify online");if(state==null)throw new IllegalStateException("no valid lease");Map<String,Object> c=verify(Json.object(state.get("lease")),keys,Json.text(cfg,"product_id"),Json.text(cfg,"device_id"),now());Capabilities.context(c,cfg,sessionID);return c;}
    private int timeout(){return cfg.containsKey("timeout_seconds")?(int)Json.number(cfg,"timeout_seconds"):10;}
    private void invalidate(){state=null;publish(false,null);try{Path p=cachePath();if(p!=null)Files.deleteIfExists(p);}catch(IOException ignored){}}
    private void save()throws Exception{Path p=cachePath();if(p==null)return;p=p.toAbsolutePath();Files.createDirectories(p.getParent());Path tmp=Files.createTempFile(p.getParent(),".lease-",".tmp");try{try{Files.setPosixFilePermissions(tmp,PosixFilePermissions.fromString("rw-------"));}catch(UnsupportedOperationException ignored){}Files.writeString(tmp,Json.write(state));view=new View(state,wall,tick);try{Files.move(tmp,p,StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}catch(AtomicMoveNotSupportedException e){Files.move(tmp,p,StandardCopyOption.REPLACE_EXISTING);}}finally{Files.deleteIfExists(tmp);}}
    private Map<String,Object>request(String op,String activation)throws Exception{return request(op,activation,UUID.randomUUID().toString(),Map.of());}
    private Map<String,Object>request(String op,String activation,String idem,Map<String,Object>extra)throws Exception{
        credential.prepare(sessionID);if(op.equals("activate")&&credential.temporary()&&state==null)idem=credential.request(idem);Map<String,Object> body=body(activation);body.putAll(extra);IOException last=null;
        for (int attempt=0; attempt<2; attempt++) {
            if (attempt>0) Thread.sleep(200);
            HttpRequest req=HttpRequest.newBuilder(URI.create(Json.text(cfg,"base_url").replaceAll("/$","")+"/api/v1/"+op))
                .timeout(Duration.ofSeconds(timeout())).header("Content-Type","application/json").header("Idempotency-Key",idem)
                .POST(HttpRequest.BodyPublishers.ofString(Json.write(body))).build();
            CompletableFuture<HttpResponse<byte[]>> pending=http.sendAsync(req, info -> new BoundedBody());
            HttpResponse<byte[]> res;
            try { res=pending.get(timeout(),TimeUnit.SECONDS); }
            catch (TimeoutException e) { pending.cancel(true);last=new IOException("runtime request timed out",e);continue; }
            catch (InterruptedException e) { pending.cancel(true);Thread.currentThread().interrupt();throw e; }
            catch (ExecutionException e) {
                if (e.getCause() instanceof IllegalArgumentException invalid) throw invalid;
                if (e.getCause() instanceof IOException io) { last=io;continue; }
                throw new IOException("runtime transport failed",e.getCause());
            }
            
            String utf8;
            try { utf8=StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(res.body())).toString(); }
            catch (CharacterCodingException e) { throw new IllegalArgumentException("invalid response UTF-8",e); }
            Map<String,Object> env;
            try { env=Json.object(Json.parse(utf8)); }
            catch (IllegalArgumentException e) {
                if(res.statusCode()!=429&&res.statusCode()<500)throw e;
                int retry=res.headers().firstValue("Retry-After").filter(v->v.matches("[0-9]{1,5}")).map(Integer::parseInt).orElse(0);
                APIException error=new APIException(res.statusCode(),"UNEXPECTED_RESPONSE",res.headers().firstValue("X-Request-ID").orElse(""),Math.min(86400,retry));
                if(res.statusCode()>=500){last=error;continue;}throw error;
            }
            if (res.statusCode()!=200||env.get("error")!=null) {
                String code=env.get("error") instanceof Map?Json.text(Json.object(env.get("error")),"code"):"UNEXPECTED_RESPONSE";
                int retry=res.headers().firstValue("Retry-After").filter(v->v.matches("[0-9]{1,5}")).map(Integer::parseInt).orElse(0);APIException error=new APIException(res.statusCode(),code,(String)env.getOrDefault("request_id",""),Math.min(86400,retry));if(res.statusCode()>=500){last=error;continue;}throw error;
            }
            return Json.object(env.get("data"));
        }
        throw last;
    }
    // Bound memory while streaming, and keep the deadline active until the entire body arrives.
    private static final class BoundedBody implements HttpResponse.BodySubscriber<byte[]> {
        private final HttpResponse.BodySubscriber<byte[]> delegate=HttpResponse.BodySubscribers.ofByteArray();
        private Flow.Subscription subscription;
        private int bytes;
        private boolean failed;
        public CompletionStage<byte[]> getBody() { return delegate.getBody(); }
        public void onSubscribe(Flow.Subscription value) { subscription=value;delegate.onSubscribe(value); }
        public void onNext(List<ByteBuffer> items) {
            if (failed) return;
            for (ByteBuffer item:items) {
                if (item.remaining()>LIMIT-bytes) {
                    failed=true;subscription.cancel();delegate.onError(new IllegalArgumentException("response size limit"));return;
                }
                bytes+=item.remaining();
            }
            delegate.onNext(items);
        }
        public void onError(Throwable error) { if (!failed) delegate.onError(error); }
        public void onComplete() { if (!failed) delegate.onComplete(); }
    }
    private synchronized Map<String,Object>call(String op,boolean forceOnline)throws Exception{
        try{Map<String,Object> c=check();if(!forceOnline&&op.equals("activate")&&RuntimePolicy.mode(c).equals("permanent")&&RuntimePolicy.heartbeat(c,3600)==0)return c;}catch(Exception ignored){}String activation="";if(!op.equals("activate")){if(state==null)throw new IllegalStateException("activate first");activation=Json.text(Json.object(Json.parse(new String(decode(Json.text(Json.object(state.get("lease")),"payload")),StandardCharsets.UTF_8))),"activation_id");}
        Map<String,Object>result;try{result=request(op,activation);}catch(Exception e){boolean transientError=e instanceof InterruptedException||e instanceof IOException&&(!(e instanceof APIException)||((APIException)e).retryable);if(transientError&&!Thread.currentThread().isInterrupted()){try{Map<String,Object> c=check();if(!forceOnline&&!RuntimePolicy.mode(c).equals("none")){publish(false,e);return c;}if(RuntimePolicy.mode(c).equals("none"))invalidate();}catch(Exception ignored){}}if(!transientError)invalidate();publish(false,e);throw e;}
        Map<String,Object>claims;try{claims=verify(Json.object(result.get("lease")),keys,Json.text(cfg,"product_id"),Json.text(cfg,"device_id"),now());Capabilities.context(claims,cfg,sessionID);if(!Json.text(claims,"activation_id").equals(Json.text(result,"activation_id")))throw new IllegalArgumentException("activation mismatch");}catch(Exception e){invalidate();throw e;}
        credential.accept(result,sessionID);wall=now();tick=System.nanoTime();state=new LinkedHashMap<>();state.put("version",1L);state.put("license_hash",hash());state.put("lease",result.get("lease"));state.put("observed_at",wall);interval=RuntimePolicy.heartbeat(claims,(int)Math.max(10,Math.min(86400,result.containsKey("heartbeat_seconds")?Json.number(result,"heartbeat_seconds"):3600)));save();publish(true,null);return claims;
    }
    public Map<String,Object>activate()throws Exception{return call("activate",false);}public Map<String,Object>validate()throws Exception{return call("validate",false);}public Map<String,Object>heartbeat()throws Exception{return call("heartbeat",false);}
    public boolean checkFeature(String name){return Boolean.TRUE.equals(featureStatus(name).get("allowed"));}
    public Map<String,Object>refreshOnline()throws Exception{return call("activate",true);}
    public synchronized void deactivate()throws Exception{boolean floating=false;try{floating=Capabilities.floating(check());}catch(Exception ignored){}String activation="";if(state!=null)activation=Json.text(Json.object(Json.parse(new String(decode(Json.text(Json.object(state.get("lease")),"payload")),StandardCharsets.UTF_8))),"activation_id");invalidate();if(!activation.isEmpty())request("deactivate",activation);if(!floating)credential.clear();publish(false,new APIException(0,"DEVICE_RELEASED"));}
    public void startHeartbeat(java.util.function.Consumer<Exception> onError){try{if(interval==0)return;}catch(Exception ignored){}if(heartbeat!=null&&heartbeat.isAlive())return;stopped=false;heartbeat=new Thread(()->{while(!stopped){if(interval==0)return;try{Thread.sleep((long)(Math.max(Math.max(600,interval)*(1+new SecureRandom().nextDouble()*.1)*1000,retryAt-System.currentTimeMillis())));if(!stopped)heartbeat();}catch(InterruptedException e){Thread.currentThread().interrupt();return;}catch(Exception e){if(!stopped&&onError!=null)onError.accept(e);}}},"licentivo-heartbeat");heartbeat.setDaemon(true);heartbeat.start();}
    private Map<String,Object>body(String activation){Map<String,Object>b=new LinkedHashMap<>();for(String k:new String[]{"product_id","license_key","device_id","device_name","app_version"})b.put(k,cfg.getOrDefault(k,""));b.put("license_key",credential.key());b.put("activation_id",activation);b.put("session_id",sessionID);b.put("device_profile",profile);return b;}
    public Map<String,Object> start(java.util.function.Consumer<Exception>onError)throws Exception{Map<String,Object> c=null;try{c=localCheck();if(RuntimePolicy.mode(c).equals("none")||Capabilities.floating(c))c=null;}catch(Exception ignored){}if(c==null){c=activate();startHeartbeat(onError);return c;}startHeartbeat(onError);refresh=new Thread(()->{try{refreshOnline();}catch(Exception e){if(!stopped&&onError!=null)onError.accept(e);}},"licentivo-startup-refresh");refresh.setDaemon(true);refresh.start();return c;}
    public static Client open(String path,java.util.function.Consumer<Exception>onError)throws Exception{Client c=new Client(Json.object(Json.parse(Files.readString(Path.of(path)))));try{c.start(onError);return c;}catch(Exception e){c.close();throw e;}}
    // Shared product settings + the customer's key, supplied by the application's activation UI.
    // Supply the customer's license key without modifying the shared product file.
    public static Client openWithLicense(String path,String licenseKey,java.util.function.Consumer<Exception>onError)throws Exception{
        Map<String,Object>cfg=new LinkedHashMap<>(Json.object(Json.parse(Files.readString(Path.of(path)))));
        cfg.put("license_key",Objects.requireNonNull(licenseKey,"licenseKey").strip());
        Client c=new Client(cfg);
        try{c.start(onError);return c;}catch(Exception e){c.close();throw e;}
    }
    public synchronized Map<String,Object> consume(String feature,long quantity,String operationID)throws Exception{if(operationID==null||operationID.isEmpty())throw new IllegalArgumentException("retain operationID for retries");Map<String,Object>c=check();try{return Json.object(request("consume",Json.text(c,"activation_id"),operationID,Map.of("feature",feature,"quantity",quantity)).get("consumption"));}catch(APIException e){if(Set.of("LICENSE_REVOKED","LICENSE_SUSPENDED","LICENSE_EXPIRED","DEVICE_RELEASED","DEVICE_CREDENTIAL_REVOKED").contains(e.code))invalidate();publish(false,e);throw e;}}
    public synchronized String offlineRequest(String operation)throws Exception{credential.prepare(sessionID);if(!Set.of("activate","deactivate").contains(operation))throw new IllegalArgumentException("invalid offline operation");String activation="";if(operation.equals("deactivate")){activation=Json.text(check(),"activation_id");invalidate();}Map<String,Object>b=body(activation);b.put("version",1L);b.put("request_id",UUID.randomUUID().toString());b.put("created_at",now());b.put("operation",operation);return Json.write(b);}
    public synchronized Map<String,Object> importOffline(String requestJSON,String responseJSON)throws Exception{Map<String,Object>req=Json.object(Json.parse(requestJSON)),res=Json.object(Json.parse(responseJSON));if(Json.number(res,"version")!=1||!"activate".equals(req.get("operation"))||!"activate".equals(res.get("operation"))||!Objects.equals(req.get("request_id"),res.get("request_id")))throw new IllegalArgumentException("offline context mismatch");for(String k:new String[]{"product_id","device_id","license_key"})if(!Objects.equals(req.get(k),body("").get(k)))throw new IllegalArgumentException("offline device mismatch");Map<String,Object>result=Json.object(res.get("result")),c=verify(Json.object(result.get("lease")),keys,Json.text(cfg,"product_id"),Json.text(cfg,"device_id"),now());Capabilities.context(c,cfg,sessionID);if(RuntimePolicy.mode(c).equals("none")||!Objects.equals(c.get("offline_request_id"),req.get("request_id"))||!Objects.equals(c.get("activation_id"),result.get("activation_id")))throw new IllegalArgumentException("offline signature context mismatch");credential.accept(result,sessionID);wall=now();tick=System.nanoTime();state=new LinkedHashMap<>();state.put("version",1L);state.put("license_hash",hash());state.put("lease",result.get("lease"));state.put("observed_at",wall);interval=RuntimePolicy.heartbeat(c,3600);save();publish(false,null);return c;}
    private Map<String,Object> localCheck()throws Exception{View v=view;if(v==null||v.state==null)throw new IllegalStateException("verification required");if(v.wall>0&&now()<v.wall+(System.nanoTime()-v.tick)/1000000000L-300)throw new IllegalStateException("clock rollback");Map<String,Object> c=verify(Json.object(v.state.get("lease")),keys,Json.text(cfg,"product_id"),Json.text(cfg,"device_id"),now());Capabilities.context(c,cfg,sessionID);return c;}
    private void publish(boolean currentOnline,Exception failure){Map<String,Object> before=status();view=new View(state,wall,tick);online=currentOnline;lastError=failure;if(failure instanceof APIException e&&e.retryAfterSeconds>0)retryAt=System.currentTimeMillis()+e.retryAfterSeconds*1000L;Map<String,Object> after=status();var listener=statusListener;if(listener!=null&&!before.equals(after))CompletableFuture.runAsync(()->listener.accept(after));}
    public void onStatusChanged(java.util.function.Consumer<Map<String,Object>> listener){statusListener=listener;}
    public Map<String,Object> status(){Map<String,Object> s=new LinkedHashMap<>();s.put("state","verification_required");s.put("allowed",false);s.put("code",lastError==null?"":lastError instanceof APIException e?e.code:"NETWORK_UNAVAILABLE");s.put("request_id",lastError instanceof APIException e?e.requestID:"");s.put("retryable",lastError!=null&&(!(lastError instanceof APIException)||((APIException)lastError).retryable));s.put("lease_valid_until",0L);try{Map<String,Object> c=localCheck();s.put("state",online?"active":"offline_valid");s.put("allowed",true);s.put("lease_valid_until",c.get("expires_at"));}catch(Exception ignored){}String mapped=Map.of("LICENSE_EXPIRED","expired","LICENSE_REVOKED","revoked","LICENSE_SUSPENDED","revoked","DEVICE_RELEASED","released","DEVICE_CREDENTIAL_REVOKED","released","FEATURE_QUOTA_EXCEEDED","quota_exhausted").get(s.get("code"));if(mapped!=null)s.put("state",mapped);return s;}
    public Map<String,Object> featureStatus(String name){Map<String,Object>s=status();if(Boolean.TRUE.equals(s.get("allowed")))try{if(!((List<?>)localCheck().get("features")).contains(name)){s.put("allowed",false);s.put("code","FEATURE_NOT_ENTITLED");}}catch(Exception e){s.put("allowed",false);s.put("state","verification_required");}return s;}
    private synchronized Map<String,Object> reservation(String op,String operationID,Map<String,Object> extra)throws Exception{if(operationID==null||operationID.isEmpty())throw new IllegalArgumentException("retain operationID for retries");Map<String,Object> c=check();try{Map<String,Object> result=Json.object(request("consume/"+op,Json.text(c,"activation_id"),operationID,extra).get("reservation"));publish(true,null);return result;}catch(Exception e){if(e instanceof APIException a&&Set.of("LICENSE_REVOKED","LICENSE_SUSPENDED","LICENSE_EXPIRED","DEVICE_RELEASED","DEVICE_CREDENTIAL_REVOKED").contains(a.code))invalidate();publish(false,e);throw e;}}
    public Map<String,Object> reserve(String feature,long quantity,String operationID)throws Exception{return reservation("reserve",operationID,Map.of("feature",feature,"quantity",quantity));}
    public Map<String,Object> commit(String reservationID,String operationID)throws Exception{return reservation("commit",operationID,Map.of("reservation_id",reservationID));}
    public Map<String,Object> cancel(String reservationID,String operationID)throws Exception{return reservation("cancel",operationID,Map.of("reservation_id",reservationID));}
    @Override public void close(){stopped=true;Thread h=heartbeat,r=refresh;heartbeat=null;refresh=null;for(Thread worker:new Thread[]{h,r})if(worker!=null)worker.interrupt();for(Thread worker:new Thread[]{h,r})if(worker!=null&&worker!=Thread.currentThread())try{worker.join(5000);}catch(InterruptedException e){Thread.currentThread().interrupt();}try{if(Capabilities.floating(check()))deactivate();}catch(Exception ignored){}}
}
