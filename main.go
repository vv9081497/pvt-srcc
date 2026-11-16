// =============================================================================
// Shopify / Multi-gateway CC Checker — Go API Server (Railway-ready)
// =============================================================================

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// -----------------------------------------------------------------------------
// Config
// -----------------------------------------------------------------------------
const (
	DefaultPort     = "8080"
	DefaultWorkers  = 200
	RequestTimeout  = 50 * time.Second
	SitesFile       = "sites.txt"
	ProxiesFile     = "proxies.txt"
)

// -----------------------------------------------------------------------------
// Global state
// -----------------------------------------------------------------------------
var (
	sitesMu   sync.RWMutex
	sites     []string
	proxiesMu sync.RWMutex
	proxies   []string

	workersBusy int64
	workersCap  int64
	workerSem   chan struct{}

	client tls_client.HttpClient
	startTime = time.Now()
)

// -----------------------------------------------------------------------------
// /health
// -----------------------------------------------------------------------------
type HealthResponse struct {
	Status      string `json:"status"`
	WorkersBusy int64  `json:"workers_busy"`
	WorkersCap  int64  `json:"workers_cap"`
	Sites       int    `json:"sites"`
	Proxies     int    `json:"proxies"`
	Uptime      int64  `json:"uptime_sec"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	sitesMu.RLock()
	siteCount := len(sites)
	sitesMu.RUnlock()

	proxiesMu.RLock()
	proxyCount := len(proxies)
	proxiesMu.RUnlock()

	resp := HealthResponse{
		Status:      "ok",
		WorkersBusy: atomic.LoadInt64(&workersBusy),
		WorkersCap:  atomic.LoadInt64(&workersCap),
		Sites:       siteCount,
		Proxies:     proxyCount,
		Uptime:      int64(time.Since(startTime).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// -----------------------------------------------------------------------------
// /stats
// -----------------------------------------------------------------------------
type StatsResponse struct {
	CPU        float64 `json:"cpu_pct"`
	RAMTotal   float64 `json:"ram_total_gb"`
	RAMUsed    float64 `json:"ram_used_gb"`
	Active     int64   `json:"active_checks"`
	WorkersCap int64   `json:"workers_cap"`
	Sites      int     `json:"sites"`
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	sitesMu.RLock()
	siteCount := len(sites)
	sitesMu.RUnlock()

	resp := StatsResponse{
		Active:     atomic.LoadInt64(&workersBusy),
		WorkersCap: atomic.LoadInt64(&workersCap),
		Sites:      siteCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// -----------------------------------------------------------------------------
// /?<card>&proxy=<proxy>
// -----------------------------------------------------------------------------
type CheckResult struct {
	Response string `json:"Response"`
	Gate     string `json:"Gate"`
	Charged  string `json:"Charged"`
	Approved string `json:"Approved"`
	Price    string `json:"Price"`
	Time     string `json:"Time"`
	Message  string `json:"message,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
	PriceN   string `json:"price,omitempty"`
	Currency string `json:"currency,omitempty"`
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	raw := r.URL.RawQuery
	parts := strings.SplitN(raw, "&", 2)

	card := ""
	proxyStr := ""
	if len(parts) > 0 {
		card = parts[0]
	}
	if len(parts) > 1 {
		for _, p := range strings.Split(parts[1], "&") {
			if strings.HasPrefix(p, "proxy=") {
				proxyStr = strings.TrimPrefix(p, "proxy=")
			}
		}
	}

	if card == "" {
		http.Error(w, "missing card", http.StatusBadRequest)
		return
	}

	// Worker slot acquire
	select {
	case workerSem <- struct{}{}:
		atomic.AddInt64(&workersBusy, 1)
		defer func() {
			<-workerSem
			atomic.AddInt64(&workersBusy, -1)
		}()
	case <-r.Context().Done():
		http.Error(w, "client cancelled", http.StatusRequestTimeout)
		return
	case <-time.After(5 * time.Second):
		http.Error(w, "at capacity", http.StatusServiceUnavailable)
		return
	}

	site := pickSite()
	proxy := proxyStr
	if proxy == "" {
		proxy = pickProxy()
	}

	ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
	defer cancel()

	result := runCheck(ctx, card, site, proxy)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// -----------------------------------------------------------------------------
// ⚠️ YAHAN AAPKO APNA PURANA SHOPIFY LOGIC PASTE KARNA HAI ⚠️
// -----------------------------------------------------------------------------
//
// Neeche wala runCheck() ek placeholder hai. Aapke purane main.go me jo
// actual Shopify checkout logic tha (variant fetch, cart add, checkout,
// payment gateway, response parse), woh is function ke andar daal dein.
//
// Function signature same rakhna:
//   func runCheck(ctx context.Context, card, site, proxy string) CheckResult
//
// Aur end me CheckResult return karna:
//   return CheckResult{
//       Response: "<gateway response>",
//       Gate:     "<gateway name>",
//       Charged:  "True/False",
//       Approved: "True/False",
//       Price:    "<amount> <currency>",
//       Time:     time.Now().Format("2006-01-02 15:04:05"),
//   }
//
// Agar aapke purane code me runCheck() ka naam kuch aur tha (jaise
// checkCard, processCheck, etc.), toh uska poora body yahan paste karein
// aur variable names adjust karein.
//
func runCheck(ctx context.Context, card, site, proxy string) CheckResult {
	result := CheckResult{
		Response: "UNKNOWN",
		Gate:     "Shopify Payments",
		Charged:  "False",
		Approved: "False",
		Price:    "0 USD",
		Time:     time.Now().Format("2006-01-02 15:04:05"),
	}

	// ---- PLACEHOLDER START ----
	// Yeh sirf isliye hai ki endpoint hang na ho.
	// Aap ise apne asli logic se replace karein.
	select {
	case <-ctx.Done():
		result.Response = "TIMEOUT"
		return result
	case <-time.After(100 * time.Millisecond):
	}
	// ---- PLACEHOLDER END ----

	return result
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------
func pickSite() string {
	sitesMu.RLock()
	defer sitesMu.RUnlock()
	if len(sites) == 0 {
		return ""
	}
	return sites[rand.Intn(len(sites))]
}

func pickProxy() string {
	proxiesMu.RLock()
	defer proxiesMu.RUnlock()
	if len(proxies) == 0 {
		return ""
	}
	return proxies[rand.Intn(len(proxies))]
}

func loadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\ufeff")
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// -----------------------------------------------------------------------------
// Main
// -----------------------------------------------------------------------------
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultPort
	}

	workers := DefaultWorkers
	if v := os.Getenv("WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workers = n
		}
	}
	atomic.StoreInt64(&workersCap, int64(workers))
	workerSem = make(chan struct{}, workers)

	var err error
	sites, err = loadLines(SitesFile)
	if err != nil {
		log.Fatalf("failed to load %s: %v", SitesFile, err)
	}
	log.Printf("[INIT] sites loaded: %d", len(sites))

	proxies, _ = loadLines(ProxiesFile)
	log.Printf("[INIT] proxies loaded: %d", len(proxies))

	client, err = tls_client.NewHttpClient(
		tls_client.NewNoopLogger(),
		tls_client.WithTimeoutSeconds(int(RequestTimeout.Seconds())),
		tls_client.WithClientProfile(profiles.Chrome_131),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithInsecureSkipVerify(),
	)
	if err != nil {
		log.Fatalf("tls client init failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/stats", statsHandler)
	mux.HandleFunc("/", checkHandler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: RequestTimeout + 10*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("[START] listening on :%s, workers=%d, sites=%d, proxies=%d",
		port, workers, len(sites), len(proxies))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// unused import guard (fhttp aapke actual logic me use hoga)
var _ = fhttp.MethodGet
var _ = fmt.Sprintf
