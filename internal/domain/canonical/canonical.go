package canonical

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ComputeBusinessPayloadHash creates a deterministic SHA-256 hash of the business payload.
// It uses canonical JSON with strictly sorted keys, excluding transport headers and idempotency key.
// Ensures identical hash computation across HTTP and SQS message envelopes.
func ComputeBusinessPayloadHash(
	providerID string,
	externalTransactionID string,
	playerID string,
	walletID string,
	roundID string,
	gameID string,
	kind string,
	amount string,
	currency string,
	referenceExternalTransactionID *string,
) (string, error) {
	// Canonical representation: key-value map with ordered keys
	m := map[string]interface{}{
		"amount":                strings.TrimSpace(amount),
		"currency":              strings.ToUpper(strings.TrimSpace(currency)),
		"externalTransactionId": strings.TrimSpace(externalTransactionID),
		"gameId":                strings.TrimSpace(gameID),
		"kind":                  strings.ToUpper(strings.TrimSpace(kind)),
		"playerId":              strings.ToLower(strings.TrimSpace(playerID)),
		"providerId":            strings.TrimSpace(providerID),
		"roundId":               strings.TrimSpace(roundID),
		"walletId":              strings.ToLower(strings.TrimSpace(walletID)),
	}

	if referenceExternalTransactionID != nil && strings.TrimSpace(*referenceExternalTransactionID) != "" {
		m["referenceExternalTransactionId"] = strings.TrimSpace(*referenceExternalTransactionID)
	}

	canonicalJSON, err := marshalCanonical(m)
	if err != nil {
		return "", fmt.Errorf("canonical marshal failed: %w", err)
	}

	hash := sha256.Sum256(canonicalJSON)
	return hex.EncodeToString(hash[:]), nil
}

func marshalCanonical(m map[string]interface{}) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		keyBytes, _ := json.Marshal(k)
		sb.Write(keyBytes)
		sb.WriteString(":")
		valBytes, err := json.Marshal(m[k])
		if err != nil {
			return nil, err
		}
		sb.Write(valBytes)
	}
	sb.WriteString("}")
	return []byte(sb.String()), nil
}
