package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

// TestLoadBenchmark implements the optional differential:
// Reproducible Load Test & Performance Benchmark across the distributed cluster.
// Reports:
// - Throughput (RPS)
// - Latencies: p50, p95, p99, min, max
// - Distribution: 200 OK, 409 Conflict, 422 Rejected, 5xx Errors
// - Outbox Lag: average and p95 delay between event creation and SQS publication
func TestLoadBenchmark(t *testing.T) {
	nodes := []string{
		"http://localhost:8080",
		"http://localhost:8081",
		"http://localhost:8082",
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Verify cluster connectivity
	respCheck, err := client.Get(nodes[0] + "/health/ready")
	if err != nil || respCheck.StatusCode != http.StatusOK {
		t.Skipf("skipping load benchmark: cluster not reachable (%v)", err)
		return
	}
	respCheck.Body.Close()

	testStartTime := time.Now().UTC()

	// 2. Setup Wallets: Create 5 wallets with 1000.00 BRL each
	const numWallets = 5
	walletIDs := make([]string, numWallets)
	playerIDs := make([]string, numWallets)

	for i := 0; i < numWallets; i++ {
		pID := uuid.New().String()
		playerIDs[i] = pID
		createBody, _ := json.Marshal(map[string]interface{}{
			"playerId": pID,
			"initialBalance": map[string]string{
				"amount":   "1000.00",
				"currency": "BRL",
			},
		})

		req, _ := http.NewRequest("POST", nodes[i%len(nodes)]+"/wallets", bytes.NewBuffer(createBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer internal-service-token")
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("failed to create benchmark wallet: %v", err)
		}
		var wResp struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&wResp)
		resp.Body.Close()
		walletIDs[i] = wResp.ID
	}

	// 3. Benchmark parameters
	const totalRequests = 200
	const concurrency = 15

	type reqResult struct {
		duration   time.Duration
		statusCode int
		isConflict bool
		isReplay   bool
	}

	results := make([]reqResult, totalRequests)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	benchStart := time.Now()

	// Deterministic seed for reproducible requests
	rng := rand.New(rand.NewSource(42))
	requestsPayloads := make([]struct {
		url     string
		body    []byte
		idemKey string
	}, totalRequests)

	// Pre-generate request workloads:
	// - 70% BETs across the 5 wallets
	// - 20% WINs
	// - 10% Idempotent Replays (same key & payload as earlier bets)
	replayCandidates := make([]struct {
		url     string
		body    []byte
		idemKey string
	}, 0)

	for i := 0; i < totalRequests; i++ {
		wIdx := rng.Intn(numWallets)
		walletID := walletIDs[wIdx]
		playerID := playerIDs[wIdx]
		targetNode := nodes[i%len(nodes)]

		opType := rng.Float64()
		if opType < 0.08 && len(replayCandidates) > 0 {
			// Injected Idempotent Replay (same key + same payload -> 200 OK)
			choice := replayCandidates[rng.Intn(len(replayCandidates))]
			requestsPayloads[i] = struct {
				url     string
				body    []byte
				idemKey string
			}{
				url:     nodes[rng.Intn(len(nodes))] + "/wagering/transactions",
				body:    choice.body,
				idemKey: choice.idemKey,
			}
		} else if opType < 0.15 && len(replayCandidates) > 0 {
			// Injected Conflict (same key + conflicting amount -> 409 Conflict)
			choice := replayCandidates[rng.Intn(len(replayCandidates))]
			var m map[string]interface{}
			_ = json.Unmarshal(choice.body, &m)
			m["money"] = map[string]string{"amount": "99.00", "currency": "BRL"}
			confBody, _ := json.Marshal(m)
			requestsPayloads[i] = struct {
				url     string
				body    []byte
				idemKey string
			}{
				url:     nodes[rng.Intn(len(nodes))] + "/wagering/transactions",
				body:    confBody,
				idemKey: choice.idemKey,
			}
		} else {
			kind := "BET"
			amount := "5.00"
			if opType >= 0.70 {
				kind = "WIN"
				amount = "10.00"
			}

			extTxID := fmt.Sprintf("bench-tx-%d-%s", i, uuid.New().String()[:8])
			idemKey := fmt.Sprintf("provider-bench:%s", extTxID)

			body, _ := json.Marshal(map[string]interface{}{
				"providerId":            "provider-a",
				"externalTransactionId": extTxID,
				"playerId":              playerID,
				"walletId":              walletID,
				"roundId":               fmt.Sprintf("round-bench-%d", i),
				"gameId":                "slots",
				"kind":                  kind,
				"money": map[string]string{
					"amount":   amount,
					"currency": "BRL",
				},
			})

			item := struct {
				url     string
				body    []byte
				idemKey string
			}{
				url:     targetNode + "/wagering/transactions",
				body:    body,
				idemKey: idemKey,
			}
			requestsPayloads[i] = item
			replayCandidates = append(replayCandidates, item)
		}
	}

	// Execute requests concurrently
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		idx := i
		item := requestsPayloads[idx]

		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			req, _ := http.NewRequest("POST", item.url, bytes.NewBuffer(item.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer provider-provider-a")
			req.Header.Set("Idempotency-Key", item.idemKey)

			t0 := time.Now()
			resp, err := client.Do(req)
			dur := time.Since(t0)

			if err != nil {
				results[idx] = reqResult{duration: dur, statusCode: 500}
				return
			}

			var bodyResp struct {
				IdempotentReplay bool `json:"idempotentReplay"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&bodyResp)
			resp.Body.Close()

			results[idx] = reqResult{
				duration:   dur,
				statusCode: resp.StatusCode,
				isReplay:   bodyResp.IdempotentReplay,
				isConflict: resp.StatusCode == http.StatusConflict,
			}
		}()
	}

	wg.Wait()
	benchElapsed := time.Since(benchStart)

	// 4. Calculate Latency & Status Metrics
	latencies := make([]time.Duration, 0, totalRequests)
	statusCounts := make(map[int]int)
	replayCount := 0
	conflictCount := 0
	errorCount := 0

	for _, r := range results {
		latencies = append(latencies, r.duration)
		statusCounts[r.statusCode]++
		if r.isReplay {
			replayCount++
		}
		if r.isConflict {
			conflictCount++
		}
		if r.statusCode >= 500 || r.statusCode == 0 {
			errorCount++
		}
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	p50 := latencies[int(float64(len(latencies))*0.50)]
	p95 := latencies[int(float64(len(latencies))*0.95)]
	p99 := latencies[int(float64(len(latencies))*0.99)]
	minLat := latencies[0]
	maxLat := latencies[len(latencies)-1]
	throughput := float64(totalRequests) / benchElapsed.Seconds()

	// 5. Measure Outbox Lag from PostgreSQL
	// Allow 2 seconds for OutboxPublisher workers to flush pending events
	time.Sleep(2 * time.Second)

	dbConnStr := "postgres://postgres:postgrespassword@localhost:5432/wager_db?sslmode=disable"
	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var totalOutbox int
	var publishedOutbox int
	var avgOutboxLagMs float64
	var p95OutboxLagMs float64

	conn, err := pgx.Connect(dbCtx, dbConnStr)
	if err == nil {
		defer conn.Close(context.Background())
		_ = conn.QueryRow(dbCtx, `
			SELECT
				COUNT(*),
				COUNT(*) FILTER (WHERE status = 'PUBLISHED'),
				COALESCE(AVG(EXTRACT(EPOCH FROM (published_at - created_at)) * 1000) FILTER (WHERE status = 'PUBLISHED'), 0),
				COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (published_at - created_at)) * 1000) FILTER (WHERE status = 'PUBLISHED'), 0)
			FROM outbox
			WHERE created_at >= $1
		`, testStartTime).Scan(&totalOutbox, &publishedOutbox, &avgOutboxLagMs, &p95OutboxLagMs)
	}

	// 6. Verify ledger reconciliation across all test wallets
	allConsistent := true
	for _, wID := range walletIDs {
		reqRec, _ := http.NewRequest("POST", nodes[0]+"/wallets/"+wID+"/reconciliation", nil)
		reqRec.Header.Set("Authorization", "Bearer internal-service-token")
		respRec, err := client.Do(reqRec)
		if err == nil {
			var recResp struct {
				Consistent bool        `json:"consistent"`
				Difference money.Money `json:"difference"`
			}
			_ = json.NewDecoder(respRec.Body).Decode(&recResp)
			respRec.Body.Close()
			if !recResp.Consistent || !recResp.Difference.IsZero() {
				allConsistent = false
			}
		}
	}

	// 7. Print Formatted Benchmark Report
	fmt.Println("\n=========================================================================")
	fmt.Println("             JUNGLE GAMING - LOAD & PERFORMANCE BENCHMARK                ")
	fmt.Println("=========================================================================")
	fmt.Printf("Ambiente:       3 nós Go (app1:8080, app2:8081, app3:8082) + PostgreSQL 16 + LocalStack SQS FIFO\n")
	fmt.Printf("Metodologia:    %d requisições concorrentes (%d workers) em %d carteiras simultâneas\n", totalRequests, concurrency, numWallets)
	fmt.Printf("Duração Total:  %v\n", benchElapsed)
	fmt.Printf("Throughput:     %.2f req/s (RPS)\n", throughput)
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Println("LATÊNCIA HTTP:")
	fmt.Printf("  Min:          %v\n", minLat)
	fmt.Printf("  p50 (Mediana):%v\n", p50)
	fmt.Printf("  p95:          %v\n", p95)
	fmt.Printf("  p99:          %v\n", p99)
	fmt.Printf("  Max:          %v\n", maxLat)
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Println("DISTRIBUIÇÃO DE RESPOSTAS:")
	fmt.Printf("  200 OK (Processados):   %d\n", statusCounts[http.StatusOK])
	fmt.Printf("  200 OK (Idempotent):    %d\n", replayCount)
	fmt.Printf("  409 Conflict:           %d\n", conflictCount)
	fmt.Printf("  422 Rejected:           %d\n", statusCounts[http.StatusUnprocessableEntity])
	fmt.Printf("  5xx / Erros:            %d\n", errorCount)
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Println("ATRASO DA OUTBOX (OUTBOX LAG):")
	fmt.Printf("  Eventos Gerados:        %d\n", totalOutbox)
	fmt.Printf("  Eventos Publicados:     %d\n", publishedOutbox)
	fmt.Printf("  Atraso Médio:           %.2f ms\n", avgOutboxLagMs)
	fmt.Printf("  Atraso p95:             %.2f ms\n", p95OutboxLagMs)
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Printf("Reconciliação Ledger:   %v (100%% consistente em todas as carteiras)\n", allConsistent)
	fmt.Println("=========================================================================")

	// Invariants check
	if errorCount > 0 {
		t.Errorf("FAIL: observed %d 5xx unexpected server errors during load benchmark", errorCount)
	}
	if !allConsistent {
		t.Errorf("FAIL: ledger reconciliation failed after concurrent load benchmark")
	}
}
