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
	path := flag.String("config", ".local/sdk-demo.json", "SDK demo config (contains a license secret)")
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
		if _, err = other.Start(ctx, nil); err == nil {
			panic("occupied seat granted")
		}
		c.Close()
		if _, err = other.Start(ctx, nil); err != nil {
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
