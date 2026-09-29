package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds load testing parameters
type Config struct {
	BaseURL     string
	Mode        string
	Concurrency int
	Duration    time.Duration
	BotToken    string
	TgID        int64
	Version     int
	CompanyID   int
	OutputFile  string
}

// Result collects metrics for a single test run
type Result struct {
	Mode         string           `json:"mode"`
	Concurrency  int              `json:"concurrency"`
	Duration     time.Duration    `json:"duration_ns"`
	DurationSec  float64          `json:"duration_sec"`
	TotalReqs    int64            `json:"total_requests"`
	SuccessReqs  int64            `json:"success_requests"`
	FailedReqs   int64            `json:"failed_requests"`
	RPS          float64          `json:"rps"`
	StatusCodes  map[int]int64    `json:"status_codes"`
	ErrorCount   map[string]int64 `json:"errors"`
	LatencyMinMs float64          `json:"latency_min_ms"`
	LatencyAvgMs float64          `json:"latency_avg_ms"`
	LatencyMaxMs float64          `json:"latency_max_ms"`
	P50Ms        float64          `json:"p50_ms"`
	P90Ms        float64          `json:"p90_ms"`
	P95Ms        float64          `json:"p95_ms"`
	P99Ms        float64          `json:"p99_ms"`
}

// GenerateSignedToken creates a valid HMAC signed token
func GenerateSignedToken(tgID int64, version int, secret string) string {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d:%d:%d", tgID, version, exp)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%d:%d:%s", tgID, version, exp, sig)
}

func main() {
	var cfg Config
	var durationSec int

	flag.StringVar(&cfg.BaseURL, "url", "http://localhost:8082", "Base URL of the target QA2A server")
	flag.StringVar(&cfg.Mode, "mode", "all", "Test mode: health, read, write, stress, all")
	flag.IntVar(&cfg.Concurrency, "c", 50, "Number of concurrent workers")
	flag.IntVar(&durationSec, "d", 10, "Duration in seconds for each test stage")
	flag.StringVar(&cfg.BotToken, "bot-token", "8364435346:AAHoKylC6rhKsvWqP6Qp-IoAIqQBPOqfZSA", "Bot token secret for HMAC signing")
	flag.Int64Var(&cfg.TgID, "tg-id", 999888777, "Telegram ID of authenticated test user")
	flag.IntVar(&cfg.Version, "version", 1, "Token version for session")
	flag.IntVar(&cfg.CompanyID, "company-id", 1, "Target company ID")
	flag.StringVar(&cfg.OutputFile, "out", "", "Output path for JSON report")
	flag.Parse()

	cfg.Duration = time.Duration(durationSec) * time.Second

	fmt.Println("================================================================================")
	fmt.Println("🚀 QA2A High-Performance Stress & Load Testing Benchmark")
	fmt.Println("================================================================================")
	fmt.Printf("🎯 Target URL    : %s\n", cfg.BaseURL)
	fmt.Printf("⚙️  Mode          : %s\n", cfg.Mode)
	fmt.Printf("👥 Concurrency   : %d workers\n", cfg.Concurrency)
	fmt.Printf("⏱️  Duration      : %v\n", cfg.Duration)
	fmt.Printf("🔑 Auth User     : tg_id=%d (ver=%d)\n", cfg.TgID, cfg.Version)
	fmt.Println("================================================================================")

	authToken := GenerateSignedToken(cfg.TgID, cfg.Version, cfg.BotToken)

	var results []Result

	switch cfg.Mode {
	case "health":
		res := runBenchmark("Baseline Health Check (GET /health)", cfg, func(client *http.Client) (*http.Response, error) {
			req, err := http.NewRequest("GET", cfg.BaseURL+"/health", nil)
			if err != nil {
				return nil, err
			}
			return client.Do(req)
		})
		results = append(results, res)

	case "read":
		res := runReadBenchmark(cfg, authToken)
		results = append(results, res)

	case "write":
		res := runWriteBenchmark(cfg, authToken)
		results = append(results, res)

	case "stress":
		results = runStressRampUp(cfg, authToken)

	case "all":
		fmt.Println("\n📊 [Stage 1/4] Running Baseline HTTP Engine Benchmark (/health)...")
		r1 := runBenchmark("Baseline Health Check (GET /health)", cfg, func(client *http.Client) (*http.Response, error) {
			req, err := http.NewRequest("GET", cfg.BaseURL+"/health", nil)
			if err != nil {
				return nil, err
			}
			return client.Do(req)
		})
		results = append(results, r1)

		time.Sleep(1 * time.Second)
		fmt.Println("\n📊 [Stage 2/4] Running Read Query Benchmark (DB-backed /api/positions & /api/balances)...")
		r2 := runReadBenchmark(cfg, authToken)
		results = append(results, r2)

		time.Sleep(1 * time.Second)
		fmt.Println("\n📊 [Stage 3/4] Running Write Transaction Benchmark (DB-backed write-offs /api/operations)...")
		r3 := runWriteBenchmark(cfg, authToken)
		results = append(results, r3)

		time.Sleep(1 * time.Second)
		fmt.Println("\n📊 [Stage 4/4] Running Concurrency Stress Ramp-Up (finding breaking point)...")
		r4 := runStressRampUp(cfg, authToken)
		results = append(results, r4...)

	default:
		fmt.Printf("❌ Unknown mode: %s. Use: health, read, write, stress, all\n", cfg.Mode)
		os.Exit(1)
	}

	printSummary(results)

	if cfg.OutputFile != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		_ = os.WriteFile(cfg.OutputFile, data, 0644)
		fmt.Printf("\n💾 Saved JSON report to: %s\n", cfg.OutputFile)
	}
}

func runReadBenchmark(cfg Config, token string) Result {
	endpoints := []string{
		"/api/positions",
		"/api/balances",
		"/api/locations",
	}

	return runBenchmark("Read Workload (/api/positions, /api/balances, /api/locations)", cfg, func(client *http.Client) (*http.Response, error) {
		ep := endpoints[rand.Intn(len(endpoints))]
		req, err := http.NewRequest("GET", cfg.BaseURL+ep, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Telegram-ID", token)
		req.Header.Set("X-Company-ID", fmt.Sprintf("%d", cfg.CompanyID))
		req.Header.Set("Accept", "application/json")
		return client.Do(req)
	})
}

func runWriteBenchmark(cfg Config, token string) Result {
	type opPayload struct {
		CompanyID    int     `json:"company_id"`
		LocationID   int     `json:"location_id"`
		Type         string  `json:"type"`
		PositionName string  `json:"position_name"`
		Quantity     float64 `json:"quantity"`
		Unit         string  `json:"unit"`
		Comment      string  `json:"comment"`
	}

	return runBenchmark("Write Workload (POST /api/operations)", cfg, func(client *http.Client) (*http.Response, error) {
		posIdx := rand.Intn(100) + 1
		payload := opPayload{
			CompanyID:    cfg.CompanyID,
			LocationID:   1,
			Type:         "writeoff",
			PositionName: fmt.Sprintf("Product %d", posIdx),
			Quantity:     0.1,
			Unit:         "kg",
			Comment:      "Load test automated write-off",
		}
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequest("POST", cfg.BaseURL+"/api/operations", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Telegram-ID", token)
		req.Header.Set("X-Company-ID", fmt.Sprintf("%d", cfg.CompanyID))
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	})
}

func runStressRampUp(cfg Config, token string) []Result {
	concurrencies := []int{25, 50, 100, 200, 400}
	var results []Result

	for _, c := range concurrencies {
		subCfg := cfg
		subCfg.Concurrency = c
		subCfg.Duration = 5 * time.Second

		name := fmt.Sprintf("Stress Ramp-up [%d concurrent users]", c)
		res := runBenchmark(name, subCfg, func(client *http.Client) (*http.Response, error) {
			// Mixed workload: 80% reads, 20% writes
			if rand.Float32() < 0.8 {
				req, _ := http.NewRequest("GET", subCfg.BaseURL+"/api/positions", nil)
				req.Header.Set("X-Telegram-ID", token)
				req.Header.Set("X-Company-ID", fmt.Sprintf("%d", subCfg.CompanyID))
				return client.Do(req)
			}
			posIdx := rand.Intn(100) + 1
			payload := map[string]interface{}{
				"company_id":    subCfg.CompanyID,
				"location_id":   1,
				"type":          "writeoff",
				"position_name": fmt.Sprintf("Product %d", posIdx),
				"quantity":      0.05,
				"unit":          "kg",
				"comment":       "Stress ramp-up write",
			}
			b, _ := json.Marshal(payload)
			req, _ := http.NewRequest("POST", subCfg.BaseURL+"/api/operations", bytes.NewReader(b))
			req.Header.Set("X-Telegram-ID", token)
			req.Header.Set("X-Company-ID", fmt.Sprintf("%d", subCfg.CompanyID))
			req.Header.Set("Content-Type", "application/json")
			return client.Do(req)
		})
		results = append(results, res)
		time.Sleep(1 * time.Second)
	}
	return results
}

func runBenchmark(name string, cfg Config, executeReq func(client *http.Client) (*http.Response, error)) Result {
	fmt.Printf("\n▶ Starting: %s\n", name)
	fmt.Printf("  Concurrency: %d workers | Duration: %v\n", cfg.Concurrency, cfg.Duration)

	transport := &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	var (
		totalReqs   int64
		successReqs int64
		failedReqs  int64

		statusCodesMu sync.Mutex
		statusCodes   = make(map[int]int64)

		errorsMu sync.Mutex
		errors   = make(map[string]int64)

		latenciesMu sync.Mutex
		latencies   []time.Duration
	)

	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	startTime := time.Now()

	// Launch workers
	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var localLatencies []time.Duration

			for {
				select {
				case <-stopChan:
					latenciesMu.Lock()
					latencies = append(latencies, localLatencies...)
					latenciesMu.Unlock()
					return
				default:
					reqStart := time.Now()
					resp, err := executeReq(client)
					elapsed := time.Since(reqStart)

					atomic.AddInt64(&totalReqs, 1)
					localLatencies = append(localLatencies, elapsed)

					if err != nil {
						atomic.AddInt64(&failedReqs, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					// Read & discard body for connection reuse
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()

					statusCodesMu.Lock()
					statusCodes[resp.StatusCode]++
					statusCodesMu.Unlock()

					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						atomic.AddInt64(&successReqs, 1)
					} else {
						atomic.AddInt64(&failedReqs, 1)
					}
				}
			}
		}()
	}

	// Wait for duration
	time.Sleep(cfg.Duration)
	close(stopChan)
	wg.Wait()

	totalDuration := time.Since(startTime)
	actualRPS := float64(totalReqs) / totalDuration.Seconds()

	// Compute latencies
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var minLat, maxLat, avgLat, p50, p90, p95, p99 time.Duration
	if len(latencies) > 0 {
		minLat = latencies[0]
		maxLat = latencies[len(latencies)-1]

		var totalLat time.Duration
		for _, l := range latencies {
			totalLat += l
		}
		avgLat = totalLat / time.Duration(len(latencies))

		p50 = latencies[int(float64(len(latencies))*0.50)]
		p90 = latencies[int(float64(len(latencies))*0.90)]
		p95 = latencies[int(float64(len(latencies))*0.95)]
		p99 = latencies[int(float64(len(latencies))*0.99)]
	}

	res := Result{
		Mode:         name,
		Concurrency:  cfg.Concurrency,
		Duration:     totalDuration,
		DurationSec:  totalDuration.Seconds(),
		TotalReqs:    totalReqs,
		SuccessReqs:  successReqs,
		FailedReqs:   failedReqs,
		RPS:          actualRPS,
		StatusCodes:  statusCodes,
		ErrorCount:   errors,
		LatencyMinMs: float64(minLat.Microseconds()) / 1000.0,
		LatencyAvgMs: float64(avgLat.Microseconds()) / 1000.0,
		LatencyMaxMs: float64(maxLat.Microseconds()) / 1000.0,
		P50Ms:        float64(p50.Microseconds()) / 1000.0,
		P90Ms:        float64(p90.Microseconds()) / 1000.0,
		P95Ms:        float64(p95.Microseconds()) / 1000.0,
		P99Ms:        float64(p99.Microseconds()) / 1000.0,
	}

	succRate := 0.0
	if res.TotalReqs > 0 {
		succRate = float64(res.SuccessReqs) * 100.0 / float64(res.TotalReqs)
	}
	fmt.Printf("  ✔ Done! Total: %d reqs | RPS: %.2f req/s | Success: %.1f%%\n",
		res.TotalReqs, res.RPS, succRate)
	fmt.Printf("  Latency: avg=%.2fms | p50=%.2fms | p95=%.2fms | p99=%.2fms | max=%.2fms\n",
		res.LatencyAvgMs, res.P50Ms, res.P95Ms, res.P99Ms, res.LatencyMaxMs)
	if len(res.StatusCodes) > 0 {
		fmt.Printf("  Status Codes: ")
		for code, cnt := range res.StatusCodes {
			fmt.Printf("[%d: %d] ", code, cnt)
		}
		fmt.Println()
	}
	if len(res.ErrorCount) > 0 {
		fmt.Printf("  Errors: ")
		for errStr, cnt := range res.ErrorCount {
			fmt.Printf("\"%s\": %d; ", errStr, cnt)
		}
		fmt.Println()
	}

	return res
}

func printSummary(results []Result) {
	fmt.Println("\n==========================================================================================")
	fmt.Println("📊 BENCHMARK SUMMARY & PERFORMANCE MATRIX")
	fmt.Println("==========================================================================================")
	fmt.Printf("%-38s | %-6s | %-9s | %-8s | %-8s | %-8s | %-8s\n",
		"Test Stage", "Users", "RPS", "Success%", "p50 (ms)", "p95 (ms)", "p99 (ms)")
	fmt.Println("------------------------------------------------------------------------------------------")

	for _, r := range results {
		succPct := 0.0
		if r.TotalReqs > 0 {
			succPct = float64(r.SuccessReqs) * 100.0 / float64(r.TotalReqs)
		}
		fmt.Printf("%-38s | %-6d | %-9.1f | %-7.1f%% | %-8.2f | %-8.2f | %-8.2f\n",
			truncate(r.Mode, 38), r.Concurrency, r.RPS, succPct, r.P50Ms, r.P95Ms, r.P99Ms)
	}
	fmt.Println("==========================================================================================")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
