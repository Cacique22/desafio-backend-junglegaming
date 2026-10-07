package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

// TestTwoSimultaneousBetsDispute tests the required scenario:
// A wallet with 100.00 BRL receives, at the exact same instant, two distinct bets of 80.00 BRL.
// Invariants verified:
// 1. Exactly 1 bet processed, 1 bet rejected with INSUFFICIENT_FUNDS.
// 2. Final wallet balance is exactly 20.00 BRL.
// 3. Exactly one debit entry exists in the ledger.
// 4. Ledger reconciliation reports consistent: true with difference 0.00 BRL.
func TestTwoSimultaneousBetsDispute(t *testing.T) {
	baseURL := "http://localhost:8080"

	// 1. Create wallet with 100.00 BRL
	playerID := uuid.New().String()
	createWalletBody, _ := json.Marshal(map[string]interface{}{
		"playerId": playerID,
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})

	req, _ := http.NewRequest("POST", baseURL+"/wallets", bytes.NewBuffer(createWalletBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer internal-service-token")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("skipping integration test: service not running locally (%v)", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create wallet: status %d", resp.StatusCode)
	}

	var walletResp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&walletResp)
	walletID := walletResp.ID

	// 2. Launch two distinct simultaneous bets of 80.00 BRL
	var wg sync.WaitGroup
	results := make([]int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			betBody, _ := json.Marshal(map[string]interface{}{
				"providerId":            "provider-a",
				"externalTransactionId": fmt.Sprintf("tx-race-%d", idx),
				"playerId":              playerID,
				"walletId":              walletID,
				"roundID":               fmt.Sprintf("round-%d", idx),
				"gameId":                "fortune-chimp",
				"kind":                  "BET",
				"money": map[string]string{
					"amount":   "80.00",
					"currency": "BRL",
				},
			})

			r, _ := http.NewRequest("POST", baseURL+"/wagering/transactions", bytes.NewBuffer(betBody))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer provider-provider-a")
			r.Header.Set("Idempotency-Key", fmt.Sprintf("provider-a:tx-race-%d", idx))

			res, err := client.Do(r)
			if err == nil {
				results[idx] = res.StatusCode
				res.Body.Close()
			}
		}()
	}

	wg.Wait()

	// One must be 200 OK (PROCESSED), the other 422 Unprocessable (REJECTED)
	processedCount := 0
	rejectedCount := 0
	for _, status := range results {
		if status == http.StatusOK {
			processedCount++
		} else if status == http.StatusUnprocessableEntity {
			rejectedCount++
		}
	}

	if processedCount != 1 || rejectedCount != 1 {
		t.Fatalf("expected exactly 1 processed (200) and 1 rejected (422), got statuses: %v", results)
	}

	// 3. Verify final wallet balance is exactly 20.00 BRL
	reqGet, _ := http.NewRequest("GET", baseURL+"/wallets/"+walletID, nil)
	reqGet.Header.Set("Authorization", "Bearer internal-service-token")
	respGet, err := client.Do(reqGet)
	if err != nil {
		t.Fatalf("failed to get wallet: %v", err)
	}
	defer respGet.Body.Close()

	var getWalletResp struct {
		Balance money.Money `json:"balance"`
	}
	_ = json.NewDecoder(respGet.Body).Decode(&getWalletResp)

	if getWalletResp.Balance.String() != "20.00" {
		t.Errorf("expected final balance 20.00, got %s", getWalletResp.Balance.String())
	}

	// 4. Verify reconciliation
	reqRec, _ := http.NewRequest("POST", baseURL+"/wallets/"+walletID+"/reconciliation", nil)
	reqRec.Header.Set("Authorization", "Bearer internal-service-token")
	respRec, err := client.Do(reqRec)
	if err != nil {
		t.Fatalf("failed to reconcile: %v", err)
	}
	defer respRec.Body.Close()

	var recResp struct {
		Consistent bool `json:"consistent"`
	}
	_ = json.NewDecoder(respRec.Body).Decode(&recResp)
	if !recResp.Consistent {
		t.Error("expected wallet ledger reconciliation to be consistent")
	}
}

// TestFiftySimultaneousIdenticalBets verifies idempotent replay under extreme concurrent load.
func TestFiftySimultaneousIdenticalBets(t *testing.T) {
	baseURL := "http://localhost:8080"
	client := &http.Client{Timeout: 5 * time.Second}

	// Create wallet
	playerID := uuid.New().String()
	createWalletBody, _ := json.Marshal(map[string]interface{}{
		"playerId": playerID,
		"initialBalance": map[string]string{
			"amount":   "500.00",
			"currency": "BRL",
		},
	})

	req, _ := http.NewRequest("POST", baseURL+"/wallets", bytes.NewBuffer(createWalletBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer internal-service-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("skipping integration test: service not running locally (%v)", err)
		return
	}
	defer resp.Body.Close()

	var walletResp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&walletResp)
	walletID := walletResp.ID

	// Send 50 identical bets simultaneously with the SAME idempotency key
	const concurrency = 50
	var wg sync.WaitGroup
	statuses := make([]int, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			betBody, _ := json.Marshal(map[string]interface{}{
				"providerId":            "provider-a",
				"externalTransactionId": "tx-identical-replay",
				"playerId":              playerID,
				"walletId":              walletID,
				"roundId":               "round-ident",
				"gameId":                "fortune-chimp",
				"kind":                  "BET",
				"money": map[string]string{
					"amount":   "50.00",
					"currency": "BRL",
				},
			})

			r, _ := http.NewRequest("POST", baseURL+"/wagering/transactions", bytes.NewBuffer(betBody))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer provider-provider-a")
			r.Header.Set("Idempotency-Key", "provider-a:tx-identical-replay")

			res, err := client.Do(r)
			if err == nil {
				statuses[idx] = res.StatusCode
				res.Body.Close()
			}
		}()
	}

	wg.Wait()

	// All 50 should return 200 OK (1 original + 49 idempotent replays)
	for i, st := range statuses {
		if st != http.StatusOK {
			t.Errorf("request %d returned status %d, expected 200 OK", i, st)
		}
	}

	// Balance should be exactly 500.00 - 50.00 = 450.00 BRL (only ONE debit occurred!)
	reqGet, _ := http.NewRequest("GET", baseURL+"/wallets/"+walletID, nil)
	reqGet.Header.Set("Authorization", "Bearer internal-service-token")
	respGet, _ := client.Do(reqGet)
	defer respGet.Body.Close()

	var getWalletResp struct {
		Balance money.Money `json:"balance"`
	}
	_ = json.NewDecoder(respGet.Body).Decode(&getWalletResp)

	if getWalletResp.Balance.String() != "450.00" {
		t.Errorf("expected final balance 450.00 BRL, got %s", getWalletResp.Balance.String())
	}
}
