package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	client "github.com/spf86/licentivo-sdk"
	"os"
	"time"
)

func main() {
	path := flag.String("config", ".local/protocol-fixture.json", "SDK demo config (contains a license secret)")
	policyTest := flag.Bool("policy-test", false, "inspect a preloaded signed policy cache")
	expectDenied := flag.Bool("expect-denied", false, "cache must not grant access")
	cacheTest := flag.Bool("cache-test", false, "run the offline-cache integration scenario")
	refreshTest := flag.Bool("refresh-test", false, "verify explicit online policy refresh")

	advanced := flag.Bool("advanced-test", false, "check metered feature consumption")
	offlineExport := flag.Bool("offline-export", false, "export first-activation request")
	offlineImport := flag.Bool("offline-import", false, "import response and check consumption")
	floating := flag.Bool("floating-test", false, "check per-process seats and orderly release")
	transfer := flag.Bool("transfer-test", false, "activate with a temporary code or retained device credential")
	transferImport := flag.Bool("transfer-import", false, "import a temporary-code offline activation response")
	improvements := flag.Bool("improvements-test", false, "credentials and quota reservations")
	startup := flag.Bool("startup-test", false, "cached startup during slow network refresh")
	logoutRetry := flag.Bool("logout-retry", false, "retry persisted device revocations without restoring access")
	logoutWait := flag.Bool("logout-wait", false, "coordinate the shared-cache logout regression")
	meteringJobs := flag.Bool("metering-jobs-test", false, "verify distinct metered job identifiers")
	meteringFeatures := flag.Bool("metering-features-test", false, "verify independent case-sensitive feature meters")
	meteringDenial := flag.Bool("metering-denial-test", false, "verify a pending job cannot bypass changed authorization")
	expiredCancel := flag.Bool("metering-expired-cancel-test", false, "verify failed business with an expired reservation")
	expiredCommit := flag.Bool("metering-expired-commit-test", false, "verify successful business is not repeated after expiry")
	meteringRecover := flag.Bool("metering-recover-test", false, "recover the recorded metered operation after restart")
	meteringBoundary := flag.Bool("metering-boundary-test", false, "verify permission and reservation deadlines at business execution")
	offlineLifecycle := flag.Bool("offline-lifecycle-test", false, "verify offline login, logout and restart recovery")
	flag.Parse()
	b, e := os.ReadFile(*path)
	if e != nil {
		panic(e)
	}
	var cfg client.Config
	if e = json.Unmarshal(b, &cfg); e != nil {
		panic(e)
	}
	c, e := client.New(cfg)
	if e != nil {
		panic(e)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if *offlineLifecycle {
		runOfflineLifecycle(c, ctx, *path)
		return
	}
	if *logoutWait {
		if !c.CheckFeature("export") {
			panic("missing initial authorization")
		}
		if err := os.WriteFile(*path+".ready", []byte("ready"), 0600); err != nil {
			panic(err)
		}
		for {
			if _, err := os.Stat(*path + ".continue"); err == nil {
				break
			}
			if ctx.Err() != nil {
				panic(ctx.Err())
			}
			time.Sleep(10 * time.Millisecond)
		}
		result, err := c.Logout(ctx)
		if err != nil || !result.LocalComplete || c.CheckFeature("export") {
			panic("logout failed")
		}
		return
	}
	if *logoutRetry {
		if err := c.RetryPendingLogouts(ctx); err != nil {
			panic(err)
		}
		if c.CheckFeature("export") {
			panic("logout restored access")
		}
		fmt.Println("Go: pending logout completed")
		return
	}

	if *meteringBoundary {
		if _, err := c.Activate(ctx); err != nil {
			panic(err)
		}
		calls := 0
		action := func() error { calls++; return nil }
		if *meteringRecover {
			for range 2 {
				if err := c.ResolveMeteredFeature(ctx, "boundary-job", !*expectDenied); err != nil {
					panic(err)
				}
			}
			err := c.RunMeteredFeature(ctx, "export", 1, "boundary-job", action)
			if (err != nil) != *expectDenied || calls != 0 {
				panic("recovery repeated business or changed its outcome")
			}
			if *expectDenied {
				if err := c.RunMeteredFeature(ctx, "export", 1, "replacement-job", action); err != nil || calls != 1 {
					panic("cancellation did not release quota")
				}
			}
		} else {
			err := c.RunMeteredFeature(ctx, "export", 1, "boundary-job", action)
			want := 1
			if *expectDenied {
				want = 0
			}
			if (err != nil) != *expectDenied || calls != want {
				panic(fmt.Sprintf("SDK executed outside authorization: calls=%d, error=%v", calls, err))
			}
		}
		fmt.Println("Go: execution boundary regression passed")
		return
	}
	if *expiredCancel || *expiredCommit {
		calls := 0
		action := func() error { calls++; return nil }
		if !*meteringRecover {
			if _, err := c.Activate(ctx); err != nil {
				panic(err)
			}
			if err := c.RunMeteredFeature(ctx, "export", 1, "expired-job", func() error {
				calls++
				if *expiredCancel {
					return fmt.Errorf("expected business failure")
				}
				return nil
			}); err == nil || calls != 1 {
				panic("business outcome was lost")
			}
		} else if *expiredCancel {
			for range 2 {
				if err := c.ResolveMeteredFeature(ctx, "expired-job", false); err != nil {
					panic(err)
				}
			}
			if c.ResolveMeteredFeature(ctx, "expired-job", true) == nil || c.RunMeteredFeature(ctx, "export", 1, "expired-job", action) == nil || calls != 0 {
				panic("canceled business was repeated or its outcome flipped")
			}
			if err := c.RunMeteredFeature(ctx, "export", 1, "replacement-job", action); err != nil || calls != 1 {
				panic("expired cancellation did not release quota")
			}
		} else if c.RunMeteredFeature(ctx, "export", 1, "expired-job", action) == nil || calls != 0 || c.ResolveMeteredFeature(ctx, "expired-job", true) == nil || c.ResolveMeteredFeature(ctx, "expired-job", false) == nil {
			panic("completed business was repeated or its outcome flipped")
		}
		fmt.Println("Go: expired task regression passed")
		return
	}
	if *meteringJobs || *meteringFeatures || *meteringDenial {
		if _, err := c.Activate(ctx); err != nil {
			panic(err)
		}
		calls := 0
		action := func() error { calls++; return nil }
		if *meteringJobs {
			for _, job := range []string{"ExportJob", "exportjob", "ExportJob"} {
				if err := c.RunMeteredFeature(ctx, "export", 1, job, action); err != nil {
					panic(err)
				}
			}
			if calls != 2 {
				panic("distinct jobs were skipped or repeated")
			}
		} else if *meteringFeatures {
			for _, feature := range []string{"export", "Export"} {
				receipt, err := c.Consume(ctx, feature, 1, feature+"-job")
				if err != nil || receipt.Used != 1 {
					panic("feature meters were combined")
				}
			}
		} else {
			if _, err := c.Reserve(ctx, "export", 1, "pending-job"); err != nil {
				panic(err)
			}
			if err := c.RunMeteredFeature(ctx, "export", 1, "pending-job", action); err == nil || calls != 0 {
				panic("pending job bypassed current authorization")
			}
		}
		fmt.Println("Go: metering regression passed")
		return
	}
	if *improvements {
		if _, err := c.Start(ctx, nil); err != nil {
			panic(err)
		}
		one, err := c.Reserve(ctx, "export", 1, "cancel-job")
		if err != nil || one.Status != "pending" || one.Consumption.Used != 0 || one.Consumption.Reserved != 1 {
			panic("reserve failed")
		}
		canceled, err := c.Cancel(ctx, one.ID, "cancel-job")
		if err != nil || canceled.Status != "canceled" || canceled.Consumption.Used != 0 {
			panic("cancel charged usage")
		}
		two, err := c.Reserve(ctx, "export", 1, "commit-job")
		if err != nil {
			panic(err)
		}
		for i := 0; i < 2; i++ {
			done, err := c.Commit(ctx, two.ID, "commit-job")
			if err != nil || done.Status != "committed" || done.Consumption.Used != 1 {
				panic("commit charged twice")
			}
		}
		replay, err := c.Reserve(ctx, "export", 1, "commit-job")
		if err != nil || replay.Status != "committed" {
			panic("business replay unsafe")
		}
		if _, err = c.Consume(ctx, "export", 1000, "quota-job"); err == nil || c.Status().Code != "FEATURE_QUOTA_EXCEEDED" || c.Status().RequestID == "" || !c.CheckFeature("export") {
			panic("typed quota status missing")
		}
		c.Close()
		cfg.LicenseKey = ""
		restored, err := client.New(cfg)
		if err != nil {
			panic(err)
		}
		defer restored.Close()
		if _, err = restored.Start(ctx, nil); err != nil {
			panic(err)
		}
		if _, err = restored.Validate(ctx); err != nil || !restored.CheckFeature("export") {
			panic("normal key not retained")
		}
		if err = restored.Deactivate(ctx); err != nil || restored.Status().Allowed {
			panic("release did not invalidate")
		}
		fmt.Println("Go: credential / reservation / typed status OK")
		return
	}
	if *startup {
		start := time.Now()
		if _, err := c.Start(ctx, nil); err != nil {
			panic(err)
		}
		if time.Since(start) > 300*time.Millisecond {
			panic("cached startup blocked on server")
		}
		time.Sleep(100 * time.Millisecond)
		start = time.Now()
		if !c.CheckFeature("export") || time.Since(start) > 300*time.Millisecond {
			panic("local feature check blocked on network")
		}
		fmt.Println("Go: cached startup and local feature checks stay responsive")
		return
	}
	if *offlineExport {
		request, err := c.OfflineRequest("activate")
		if err != nil {
			panic(err)
		}
		if err = os.WriteFile(*path+".request.json", request, 0600); err != nil {
			panic(err)
		}
		fmt.Println("Go: offline request exported")
		return
	}
	if *transfer || *transferImport {
		if *transferImport {
			request, err := os.ReadFile(*path + ".request.json")
			if err != nil {
				panic(err)
			}
			response, err := os.ReadFile(*path + ".response.json")
			if err != nil {
				panic(err)
			}
			if _, err = c.ImportOffline(request, response); err != nil {
				panic(err)
			}
		} else if _, e = c.Start(ctx, nil); e != nil {
			panic(e)
		}
		if _, e = c.Validate(ctx); e != nil {
			panic(e)
		}
		if _, e = c.Heartbeat(ctx); e != nil {
			panic(e)
		}
		if !c.CheckFeature("export") {
			panic("feature denied")
		}
		fmt.Println("Go: transferred device credential OK")
		return
	}
	if *floating {
		if _, e = c.Start(ctx, nil); e != nil {
			panic(e)
		}
		other, err := client.New(cfg)
		if err != nil {
			panic(err)
		}
		defer other.Close()
		if other.CheckFeature("export") {
			panic("session cache reused")
		}
		if _, err = other.Start(ctx, nil); err != nil {
			panic(err)
		}
		c.Close()
		if _, err = other.Heartbeat(ctx); err != nil {
			panic(err)
		}
		if !other.CheckFeature("export") {
			panic("new session denied")
		}
		fmt.Println("Go: floating seat release OK")
		return
	}
	if *advanced || *offlineImport {
		if *offlineImport {
			request, err := os.ReadFile(*path + ".request.json")
			if err != nil {
				panic(err)
			}
			response, err := os.ReadFile(*path + ".response.json")
			if err != nil {
				panic(err)
			}
			if _, err = c.ImportOffline(request, response); err != nil {
				panic(err)
			}
		} else if _, e = c.Start(ctx, nil); e != nil {
			panic(e)
		}
		if !c.CheckFeature("export") {
			panic("feature denied")
		}
		one, err := c.Consume(ctx, "export", 1, "business-job-1")
		if err != nil || one.Used != 1 {
			panic("consume failed")
		}
		again, err := c.Consume(ctx, "export", 1, "business-job-1")
		if err != nil || again.Used != 1 {
			panic("duplicate consumption")
		}
		over, err := c.Consume(ctx, "export", 3, "business-job-2")
		if err != nil || over.Used != 4 || over.Overage != 1 {
			panic("overage failed")
		}
		if _, err = c.Consume(ctx, "export", 1, "business-job-3"); err == nil || !c.CheckFeature("export") {
			panic("quota denial failed")
		}
		if err = c.Deactivate(ctx); err != nil {
			panic(err)
		}
		fmt.Println("Go: offline/metered feature checks OK")
		return
	}
	if *refreshTest {
		if _, err := c.RefreshOnline(ctx); err == nil || !c.CheckFeature("export") {
			panic("refresh failure did not retain offline access")
		}
		if _, err := c.RefreshOnline(ctx); err != nil || !c.CheckFeature("updated") || c.CheckFeature("export") {
			panic("online refresh did not replace cached policy")
		}
		if _, err := c.RefreshOnline(ctx); err == nil || c.CheckFeature("updated") {
			panic("refresh denial did not clear cache")
		}
		fmt.Println("Go: online refresh OK")
		return
	}
	if *policyTest {
		if c.CheckFeature("export") == *expectDenied {
			panic("unexpected cache device/policy access")
		}
		if !*expectDenied {
			for _, op := range []func(context.Context) (client.Claims, error){c.Activate, c.Validate, c.Heartbeat} {
				if _, err := op(ctx); err != nil {
					panic(err)
				}
			}
		}
		fmt.Println("Go: policy cache OK")
		return
	}
	claims, e := c.Activate(ctx)
	if e != nil {
		panic(e)
	}
	fmt.Println("Go: activated", claims.ActivationID, "features", claims.Features)
	if *cacheTest {
		c.Close()
		c, e = client.New(cfg)
		if e != nil {
			panic(e)
		}
		defer c.Close()
		if _, e = c.Validate(ctx); e != nil || !c.CheckFeature("export") {
			panic("cached lease was not restored during server outage")
		}
		if _, e = c.Validate(ctx); e == nil || c.CheckFeature("export") {
			panic("server rejection did not clear cached access")
		}
		c.Close()
		reopened, err := client.New(cfg)
		if err != nil {
			panic(err)
		}
		defer reopened.Close()
		if reopened.CheckFeature("export") {
			panic("rejected lease survived restart")
		}
		fmt.Println("Go: cache restored on 503; 403 invalidated access and persisted cache")
		return
	}
	if _, e = c.Validate(ctx); e != nil {
		panic(e)
	}
	if _, e = c.Heartbeat(ctx); e != nil {
		panic(e)
	}
	fmt.Println("Go: export enabled", c.CheckFeature("export"))
	if e = c.Deactivate(ctx); e != nil {
		panic(e)
	}
	fmt.Println("Go: deactivated")
}

func runOfflineLifecycle(c *client.Client, ctx context.Context, path string) {
	read := func(suffix string) []byte {
		b, err := os.ReadFile(path + suffix)
		if err != nil {
			panic(err)
		}
		return b
	}
	write := func(suffix string, b []byte, err error) {
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(path+suffix, b, 0600); err != nil {
			panic(err)
		}
	}
	importFile := func(fresh bool) error {
		prefix := ""
		if fresh {
			prefix = ".fresh"
		}
		_, err := c.ImportOffline(read(prefix+".request.json"), read(prefix+".response.json"))
		return err
	}
	allowed := func() {
		if !c.CheckFeature("export") || c.Status().State != "offline_valid" {
			panic("offline import did not publish offline access")
		}
	}
	phase := string(read(".phase"))
	switch phase {
	case "export":
		b, err := c.OfflineRequest("activate")
		write(".request.json", b, err)
	case "import", "retry":
		if err := importFile(false); err != nil {
			panic(err)
		}
		if phase == "retry" {
			if err := importFile(false); err != nil {
				panic(err)
			}
		}
		allowed()
	case "fail_import":
		if importFile(false) == nil || c.CheckFeature("export") || c.Status().Allowed {
			panic("failed import granted protected access")
		}
	case "release":
		if err := importFile(false); err != nil {
			panic(err)
		}
		b, err := c.OfflineRequest("deactivate")
		write(".release.json", b, err)
		if c.CheckFeature("export") {
			panic("offline release retained access")
		}
	case "signedout":
		_, activationErr := c.Activate(ctx)
		_, exportErr := c.OfflineRequest("activate")
		if c.CheckFeature("export") || activationErr == nil || exportErr == nil || importFile(false) == nil {
			panic("offline release did not survive restart")
		}
	case "logout":
		if err := importFile(false); err != nil {
			panic(err)
		}
		result, err := c.Logout(ctx)
		if err != nil || !result.LocalComplete || result.RevocationPending || c.CheckFeature("export") {
			panic("logout did not complete")
		}
	case "old_export", "renew_request":
		if phase == "old_export" && (importFile(false) == nil || c.CheckFeature("export")) {
			panic("old approval restored logged-out access")
		}
		b, err := c.OfflineRequest("activate")
		write(".fresh.request.json", b, err)
		if phase == "renew_request" && (importFile(false) == nil || c.CheckFeature("export")) {
			panic("old approval replaced a newer request")
		}
	case "fresh_import":
		if err := importFile(true); err != nil {
			panic(err)
		}
		allowed()
		if importFile(false) == nil {
			panic("old approval rolled back a fresh login")
		}
		allowed()
		if err := importFile(true); err != nil {
			panic(err)
		}
		allowed()
	default:
		panic("unknown offline lifecycle phase")
	}
	fmt.Println("Go: offline lifecycle phase passed", phase)
}
