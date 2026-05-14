package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/csenet/noc-tracker/iap"
	"github.com/csenet/noc-tracker/instanton"
	"github.com/csenet/noc-tracker/tracker"
	"github.com/csenet/noc-tracker/web"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("[main] no .env loaded: %v", err)
	}

	storePath := envOr("NOC_STORE_PATH", "registrations.json")
	positionsPath := envOr("NOC_AP_POSITIONS_PATH", "ap-positions.json")
	listen := envOr("NOC_LISTEN", ":8080")
	interval := envDuration("NOC_POLL_INTERVAL", 30*time.Second)

	store, err := tracker.NewStore(storePath)
	if err != nil {
		log.Fatalf("[main] open store %s: %v", storePath, err)
	}

	positions, err := tracker.NewPositionStore(positionsPath)
	if err != nil {
		log.Fatalf("[main] open positions %s: %v", positionsPath, err)
	}

	var instantOn tracker.InstantOnSource
	if u, p := os.Getenv("ARUBA_USERNAME"), os.Getenv("ARUBA_PASSWORD"); u != "" && p != "" {
		instantOn = instanton.NewClient(u, p)
		log.Printf("[main] Instant On source enabled (%s)", u)
	} else {
		log.Printf("[main] Instant On disabled (ARUBA_USERNAME/PASSWORD unset)")
	}

	var iapClient tracker.IAPSource
	if host := os.Getenv("IAP_HOST"); host != "" {
		iapClient = iap.NewClient(
			host,
			os.Getenv("IAP_PORT"),
			envOr("IAP_USERNAME", "admin"),
			os.Getenv("IAP_PASSWORD"),
		)
		log.Printf("[main] IAP source enabled (%s)", host)
	} else {
		log.Printf("[main] IAP disabled (IAP_HOST unset)")
	}

	if instantOn == nil && iapClient == nil {
		log.Printf("[main] WARNING: no data source configured; UI will only show registrations")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr := tracker.New(instantOn, iapClient, interval)
	tr.Start(ctx)

	adminToken := os.Getenv("NOC_ADMIN_TOKEN")
	if adminToken != "" {
		log.Printf("[main] admin token enabled — editing requires ?admin=<token>")
	} else {
		log.Printf("[main] no admin token set — anyone can edit AP positions")
	}

	// Log which floorplan file (if any) the embedded static FS will serve.
	// Helps diagnose "I put my image but nothing shows up" — usually means
	// the server wasn't rebuilt or the filename didn't match.
	for _, name := range []string{"floorplan.png", "floorplan.jpg", "floorplan.jpeg", "floorplan.webp", "floorplan.svg"} {
		if _, err := os.Stat("web/static/" + name); err == nil {
			log.Printf("[main] floorplan: %s found, will be served at /floorplan", name)
			goto floorplanDone
		}
	}
	log.Printf("[main] floorplan: no web/static/floorplan.{png,jpg,jpeg,webp,svg} found — map will show empty background")
floorplanDone:

	srv := &http.Server{
		Addr:              listen,
		Handler:           web.NewServer(store, tr, positions, adminToken).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[main] shutting down")
		shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		_ = srv.Shutdown(shutdownCtx)
		cancel()
	}()

	log.Printf("[main] listening on %s, store=%s, interval=%s", listen, storePath, interval)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[main] server: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	log.Printf("[main] invalid %s=%q, using %s", key, v, fallback)
	return fallback
}
