package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
)

func TestFullLifecycleAndMultiInstanceE2E(t *testing.T) {
	app1 := "http://localhost:8080"
	app2 := "http://localhost:8081"
	app3 := "http://localhost:8082"

	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: Security - Unauthorized request must return 401
	{
		req, _ := http.NewRequest("GET", app1+"/wallets/"+uuid.New().String(), nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Skipf("cluster not reachable: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for missing auth header, got %d", resp.StatusCode)
		}
	}

	// Step 2: Security - Provider token attempting to create a wallet must return 403 Forbidden
	{
		body, _ := json.Marshal(map[string]interface{}{
			"playerId": uuid.New().String(),
			"initialBalance": map[string]string{
				"amount":   "100.00",
				"currency": "BRL",
			},
		})
		req, _ := http.NewRequest("POST", app1+"/wallets", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer provider-test")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed to do request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden when provider calls /wallets, got %d", resp.StatusCode)
		}
	}

	// Step 3: Create Wallet on app1 with 200.00 BRL using internal token
	playerID := uuid.New().String()
	createWalletBody, _ := json.Marshal(map[string]interface{}{
		"playerId": playerID,
		"initialBalance": map[string]string{
			"amount":   "200.00",
			"currency": "BRL",
		},
	})
	reqCreate, _ := http.NewRequest("POST", app1+"/wallets", bytes.NewBuffer(createWalletBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.Header.Set("Authorization", "Bearer internal-service-token")
	respCreate, err := client.Do(reqCreate)
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
	defer respCreate.Body.Close()
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", respCreate.StatusCode)
	}
	var walletResp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(respCreate.Body).Decode(&walletResp)
	walletID := walletResp.ID

	// Step 4: Security - Provider isolation violation
	// Provider token 'provider-evolution' trying to send wager with providerId 'pragmatic' -> 403
	{
		wagerBody, _ := json.Marshal(map[string]interface{}{
			"providerId":            "pragmatic",
			"externalTransactionId": "ext-iso-1",
			"playerId":              playerID,
			"walletId":              walletID,
			"roundID":               "round-1",
			"gameId":                "roulette",
			"kind":                  "BET",
			"money": map[string]string{
				"amount":   "10.00",
				"currency": "BRL",
			},
		})
		reqIso, _ := http.NewRequest("POST", app1+"/wagering/transactions", bytes.NewBuffer(wagerBody))
		reqIso.Header.Set("Content-Type", "application/json")
		reqIso.Header.Set("Authorization", "Bearer provider-evolution")
		reqIso.Header.Set("Idempotency-Key", "idem-iso-1")
		respIso, err := client.Do(reqIso)
		if err != nil {
			t.Fatalf("failed to do request: %v", err)
		}
		respIso.Body.Close()
		if respIso.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for provider isolation violation, got %d", respIso.StatusCode)
		}
	}

	// Step 5: Execute BET of 50.00 BRL on app1
	betExtID := fmt.Sprintf("ext-bet-%s", uuid.New().String()[:8])
	betIdemKey := fmt.Sprintf("idem-bet-%s", uuid.New().String()[:8])
	betPayload := map[string]interface{}{
		"providerId":            "evolution",
		"externalTransactionId": betExtID,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "blackjack",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}
	betBodyBytes, _ := json.Marshal(betPayload)

	reqBet, _ := http.NewRequest("POST", app1+"/wagering/transactions", bytes.NewBuffer(betBodyBytes))
	reqBet.Header.Set("Content-Type", "application/json")
	reqBet.Header.Set("Authorization", "Bearer provider-evolution")
	reqBet.Header.Set("Idempotency-Key", betIdemKey)
	respBet, err := client.Do(reqBet)
	if err != nil {
		t.Fatalf("bet request failed: %v", err)
	}
	defer respBet.Body.Close()
	if respBet.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for BET, got %d", respBet.StatusCode)
	}

	// Step 6: Idempotent Replay on app2 (different instance!)
	// Exact same BET request to app2 should return 200 OK with idempotentReplay: true
	reqReplay, _ := http.NewRequest("POST", app2+"/wagering/transactions", bytes.NewBuffer(betBodyBytes))
	reqReplay.Header.Set("Content-Type", "application/json")
	reqReplay.Header.Set("Authorization", "Bearer provider-evolution")
	reqReplay.Header.Set("Idempotency-Key", betIdemKey)
	respReplay, err := client.Do(reqReplay)
	if err != nil {
		t.Fatalf("replay request failed: %v", err)
	}
	defer respReplay.Body.Close()
	if respReplay.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for replay, got %d", respReplay.StatusCode)
	}
	var replayResp struct {
		IdempotentReplay bool `json:"idempotentReplay"`
	}
	_ = json.NewDecoder(respReplay.Body).Decode(&replayResp)
	if !replayResp.IdempotentReplay {
		t.Errorf("expected idempotentReplay: true, got false")
	}

	// Step 7: Idempotency Conflict on app3
	// Same idempotency key with conflicting amount (75.00 BRL instead of 50.00 BRL) -> 409 Conflict
	conflictPayload := map[string]interface{}{
		"providerId":            "evolution",
		"externalTransactionId": betExtID,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "blackjack",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "75.00",
			"currency": "BRL",
		},
	}
	conflictBodyBytes, _ := json.Marshal(conflictPayload)
	reqConflict, _ := http.NewRequest("POST", app3+"/wagering/transactions", bytes.NewBuffer(conflictBodyBytes))
	reqConflict.Header.Set("Content-Type", "application/json")
	reqConflict.Header.Set("Authorization", "Bearer provider-evolution")
	reqConflict.Header.Set("Idempotency-Key", betIdemKey)
	respConflict, err := client.Do(reqConflict)
	if err != nil {
		t.Fatalf("conflict request failed: %v", err)
	}
	respConflict.Body.Close()
	if respConflict.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 Conflict for mismatched payload with same idempotency key, got %d", respConflict.StatusCode)
	}

	// Step 8: WIN of 100.00 BRL on app2
	winExtID := fmt.Sprintf("ext-win-%s", uuid.New().String()[:8])
	winIdemKey := fmt.Sprintf("idem-win-%s", uuid.New().String()[:8])
	winPayload := map[string]interface{}{
		"providerId":            "evolution",
		"externalTransactionId": winExtID,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "round-1",
		"gameId":                "blackjack",
		"kind":                  "WIN",
		"money": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	}
	winBodyBytes, _ := json.Marshal(winPayload)
	reqWin, _ := http.NewRequest("POST", app2+"/wagering/transactions", bytes.NewBuffer(winBodyBytes))
	reqWin.Header.Set("Content-Type", "application/json")
	reqWin.Header.Set("Authorization", "Bearer provider-evolution")
	reqWin.Header.Set("Idempotency-Key", winIdemKey)
	respWin, err := client.Do(reqWin)
	if err != nil {
		t.Fatalf("win request failed: %v", err)
	}
	respWin.Body.Close()
	if respWin.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for WIN, got %d", respWin.StatusCode)
	}

	// Step 9: REFUND of initial BET on app3 (referencing betExtID)
	refundExtID := fmt.Sprintf("ext-ref-%s", uuid.New().String()[:8])
	refundIdemKey := fmt.Sprintf("idem-ref-%s", uuid.New().String()[:8])
	refundPayload := map[string]interface{}{
		"providerId":                     "evolution",
		"externalTransactionId":          refundExtID,
		"referenceExternalTransactionId": betExtID,
		"playerId":                       playerID,
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"gameId":                         "blackjack",
		"kind":                           "REFUND",
		"money": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}
	refundBodyBytes, _ := json.Marshal(refundPayload)
	reqRefund, _ := http.NewRequest("POST", app3+"/wagering/transactions", bytes.NewBuffer(refundBodyBytes))
	reqRefund.Header.Set("Content-Type", "application/json")
	reqRefund.Header.Set("Authorization", "Bearer provider-evolution")
	reqRefund.Header.Set("Idempotency-Key", refundIdemKey)
	respRefund, err := client.Do(reqRefund)
	if err != nil {
		t.Fatalf("refund request failed: %v", err)
	}
	respRefund.Body.Close()
	if respRefund.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for REFUND, got %d", respRefund.StatusCode)
	}

	// Step 10: Double REFUND prevention - Attempting a second REFUND for the same bet must fail
	dupRefundExtID := fmt.Sprintf("ext-ref-dup-%s", uuid.New().String()[:8])
	dupRefundIdemKey := fmt.Sprintf("idem-ref-dup-%s", uuid.New().String()[:8])
	dupRefundPayload := map[string]interface{}{
		"providerId":                     "evolution",
		"externalTransactionId":          dupRefundExtID,
		"referenceExternalTransactionId": betExtID,
		"playerId":                       playerID,
		"walletId":                       walletID,
		"roundId":                        "round-1",
		"gameId":                         "blackjack",
		"kind":                           "REFUND",
		"money": map[string]string{
			"amount":   "50.00",
			"currency": "BRL",
		},
	}
	dupRefundBodyBytes, _ := json.Marshal(dupRefundPayload)
	reqDupRef, _ := http.NewRequest("POST", app1+"/wagering/transactions", bytes.NewBuffer(dupRefundBodyBytes))
	reqDupRef.Header.Set("Content-Type", "application/json")
	reqDupRef.Header.Set("Authorization", "Bearer provider-evolution")
	reqDupRef.Header.Set("Idempotency-Key", dupRefundIdemKey)
	respDupRef, err := client.Do(reqDupRef)
	if err != nil {
		t.Fatalf("dup refund request failed: %v", err)
	}
	respDupRef.Body.Close()
	if respDupRef.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 Unprocessable Entity for duplicate reversal, got %d", respDupRef.StatusCode)
	}

	// Step 11: Verify Final Wallet Balance:
	// Initial: 200.00 - BET: 50.00 + WIN: 100.00 + REFUND: 50.00 = 300.00 BRL
	reqGet, _ := http.NewRequest("GET", app1+"/wallets/"+walletID, nil)
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
	if getWalletResp.Balance.String() != "300.00" {
		t.Errorf("expected balance 300.00, got %s", getWalletResp.Balance.String())
	}

	// Step 12: Verify Ledger List on app2
	reqLedger, _ := http.NewRequest("GET", app2+"/wallets/"+walletID+"/ledger", nil)
	reqLedger.Header.Set("Authorization", "Bearer internal-service-token")
	respLedger, err := client.Do(reqLedger)
	if err != nil {
		t.Fatalf("failed to list ledger: %v", err)
	}
	defer respLedger.Body.Close()
	var ledgerResp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.NewDecoder(respLedger.Body).Decode(&ledgerResp)
	// Opening (200.00) + BET (50.00) + WIN (100.00) + REFUND (50.00) = 4 ledger entries
	if len(ledgerResp.Items) != 4 {
		t.Errorf("expected exactly 4 ledger entries, got %d", len(ledgerResp.Items))
	}

	// Step 13: Verify Ledger Reconciliation on app3
	// Sum(Credits) - Sum(Debits) must equal Stored Balance (300.00 BRL) with difference 0.00 BRL
	reqRec, _ := http.NewRequest("POST", app3+"/wallets/"+walletID+"/reconciliation", nil)
	reqRec.Header.Set("Authorization", "Bearer internal-service-token")
	respRec, err := client.Do(reqRec)
	if err != nil {
		t.Fatalf("failed to reconcile: %v", err)
	}
	defer respRec.Body.Close()
	var recResp struct {
		Consistent        bool        `json:"consistent"`
		StoredBalance     money.Money `json:"storedBalance"`
		CalculatedBalance money.Money `json:"calculatedBalance"`
		Difference        money.Money `json:"difference"`
	}
	_ = json.NewDecoder(respRec.Body).Decode(&recResp)
	if !recResp.Consistent {
		t.Errorf("expected reconciliation to be consistent, got false (diff: %s)", recResp.Difference.String())
	}
	if recResp.StoredBalance.String() != "300.00" || recResp.CalculatedBalance.String() != "300.00" {
		t.Errorf("expected stored=300.00 and calculated=300.00, got %s and %s", recResp.StoredBalance.String(), recResp.CalculatedBalance.String())
	}
	if recResp.Difference.String() != "0.00" {
		t.Errorf("expected difference 0.00, got %s", recResp.Difference.String())
	}
}

func TestOutOfOrderRefundResolution(t *testing.T) {
	baseURL := "http://localhost:8080"
	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Create wallet with 100.00 BRL
	playerID := uuid.New().String()
	createBody, _ := json.Marshal(map[string]interface{}{
		"playerId": playerID,
		"initialBalance": map[string]string{
			"amount":   "100.00",
			"currency": "BRL",
		},
	})
	reqCreate, _ := http.NewRequest("POST", baseURL+"/wallets", bytes.NewBuffer(createBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.Header.Set("Authorization", "Bearer internal-service-token")
	respCreate, err := client.Do(reqCreate)
	if err != nil {
		t.Skipf("service not available: %v", err)
		return
	}
	defer respCreate.Body.Close()
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create wallet: %d", respCreate.StatusCode)
	}
	var walletResp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(respCreate.Body).Decode(&walletResp)
	walletID := walletResp.ID

	targetBetExtID := fmt.Sprintf("bet-future-%s", uuid.New().String()[:8])
	refundExtID := fmt.Sprintf("ref-early-%s", uuid.New().String()[:8])

	// 2. REFUND arrives BEFORE the BET!
	// Must return 202 Accepted with status PENDING_REFERENCE
	refundBody, _ := json.Marshal(map[string]interface{}{
		"providerId":                     "evolution",
		"externalTransactionId":          refundExtID,
		"referenceExternalTransactionId": targetBetExtID,
		"playerId":                       playerID,
		"walletId":                       walletID,
		"roundId":                        "round-ooo",
		"gameId":                         "blackjack",
		"kind":                           "REFUND",
		"money": map[string]string{
			"amount":   "25.00",
			"currency": "BRL",
		},
	})
	reqRef, _ := http.NewRequest("POST", baseURL+"/wagering/transactions", bytes.NewBuffer(refundBody))
	reqRef.Header.Set("Content-Type", "application/json")
	reqRef.Header.Set("Authorization", "Bearer provider-evolution")
	reqRef.Header.Set("Idempotency-Key", "idem-early-ref-"+refundExtID)
	respRef, err := client.Do(reqRef)
	if err != nil {
		t.Fatalf("refund request failed: %v", err)
	}
	defer respRef.Body.Close()
	if respRef.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for early refund, got %d", respRef.StatusCode)
	}
	var refResult struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(respRef.Body).Decode(&refResult)
	if refResult.Status != "PENDING_REFERENCE" {
		t.Fatalf("expected status PENDING_REFERENCE, got %s", refResult.Status)
	}

	// 3. Now the BET arrives!
	betBody, _ := json.Marshal(map[string]interface{}{
		"providerId":            "evolution",
		"externalTransactionId": targetBetExtID,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "round-ooo",
		"gameId":                "blackjack",
		"kind":                  "BET",
		"money": map[string]string{
			"amount":   "25.00",
			"currency": "BRL",
		},
	})
	reqBet, _ := http.NewRequest("POST", baseURL+"/wagering/transactions", bytes.NewBuffer(betBody))
	reqBet.Header.Set("Content-Type", "application/json")
	reqBet.Header.Set("Authorization", "Bearer provider-evolution")
	reqBet.Header.Set("Idempotency-Key", "idem-bet-"+targetBetExtID)
	respBet, err := client.Do(reqBet)
	if err != nil {
		t.Fatalf("bet request failed: %v", err)
	}
	defer respBet.Body.Close()
	if respBet.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for bet, got %d", respBet.StatusCode)
	}

	// 4. Wait for PendingReferenceResolver background worker to process the early refund (polls every 2s)
	time.Sleep(3 * time.Second)

	// 5. Query the early refund status: should now be PROCESSED!
	reqCheck, _ := http.NewRequest("GET", fmt.Sprintf("%s/providers/evolution/wagering/transactions/%s", baseURL, refundExtID), nil)
	reqCheck.Header.Set("Authorization", "Bearer provider-evolution")
	respCheck, err := client.Do(reqCheck)
	if err != nil {
		t.Fatalf("check refund request failed: %v", err)
	}
	defer respCheck.Body.Close()
	if respCheck.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK checking refund, got %d", respCheck.StatusCode)
	}
	var checkResult struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(respCheck.Body).Decode(&checkResult)
	if checkResult.Status != "PROCESSED" {
		t.Errorf("expected early refund to be resolved to PROCESSED, got %s", checkResult.Status)
	}

	// 6. Check final wallet balance: Initial (100.00) - BET (25.00) + REFUND (25.00) = 100.00 BRL
	reqGet, _ := http.NewRequest("GET", baseURL+"/wallets/"+walletID, nil)
	reqGet.Header.Set("Authorization", "Bearer internal-service-token")
	respGet, err := client.Do(reqGet)
	if err != nil {
		t.Fatalf("get wallet failed: %v", err)
	}
	defer respGet.Body.Close()
	var balResult struct {
		Balance money.Money `json:"balance"`
	}
	_ = json.NewDecoder(respGet.Body).Decode(&balResult)
	if balResult.Balance.String() != "100.00" {
		t.Errorf("expected final balance 100.00, got %s", balResult.Balance.String())
	}
}
