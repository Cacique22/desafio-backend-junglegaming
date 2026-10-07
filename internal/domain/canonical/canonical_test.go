package canonical_test

import (
	"testing"

	"github.com/junglegaming/backend-challenge-go/internal/domain/canonical"
)

func TestComputeBusinessPayloadHash_Deterministic(t *testing.T) {
	ref := "ref-123"
	hash1, err := canonical.ComputeBusinessPayloadHash(
		"provider-a", "tx-1", "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
		"0192f291-27dd-7d3f-8071-5f8685deef37", "round-1", "game-1",
		"BET", "25.00", "BRL", &ref,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hash2, err := canonical.ComputeBusinessPayloadHash(
		"provider-a", "tx-1", "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
		"0192f291-27dd-7d3f-8071-5f8685deef37", "round-1", "game-1",
		"BET", "25.00", "BRL", &ref,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("expected deterministic hash, got %s and %s", hash1, hash2)
	}

	// Changing amount should yield different hash
	hash3, _ := canonical.ComputeBusinessPayloadHash(
		"provider-a", "tx-1", "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
		"0192f291-27dd-7d3f-8071-5f8685deef37", "round-1", "game-1",
		"BET", "30.00", "BRL", &ref,
	)

	if hash1 == hash3 {
		t.Errorf("different amounts must produce different hashes")
	}
}
