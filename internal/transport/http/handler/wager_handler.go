package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/domain/wager"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
	"github.com/junglegaming/backend-challenge-go/internal/transport/http/middleware"
	"github.com/junglegaming/backend-challenge-go/internal/usecase"
)

type WagerHandler struct {
	logger       *slog.Logger
	wagerUseCase *usecase.WagerUseCase
	txRepo       *repository.TransactionRepository
}

func NewWagerHandler(logger *slog.Logger, wagerUseCase *usecase.WagerUseCase, txRepo *repository.TransactionRepository) *WagerHandler {
	return &WagerHandler{
		logger:       logger,
		wagerUseCase: wagerUseCase,
		txRepo:       txRepo,
	}
}

type ProcessWagerRequest struct {
	ProviderID                     string      `json:"providerId"`
	ExternalTransactionID          string      `json:"externalTransactionId"`
	PlayerID                       string      `json:"playerId"`
	WalletID                       string      `json:"walletId"`
	RoundID                        string      `json:"roundId"`
	GameID                         string      `json:"gameId"`
	Kind                           string      `json:"kind"`
	Money                          money.Money `json:"money"`
	ReferenceExternalTransactionID *string     `json:"referenceExternalTransactionId,omitempty"`
}

func (h *WagerHandler) Process(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		http.Error(w, `{"error":"missing required Idempotency-Key header"}`, http.StatusBadRequest)
		return
	}

	var req ProcessWagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	// Provider isolation check: authenticated provider must match payload providerId
	authProvider := middleware.GetProviderID(r.Context())
	if authProvider != "" && authProvider != "SYSTEM" && authProvider != req.ProviderID {
		http.Error(w, `{"error":"forbidden: provider isolation violation"}`, http.StatusForbidden)
		return
	}

	playerUUID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		http.Error(w, `{"error":"invalid playerId UUID"}`, http.StatusBadRequest)
		return
	}

	walletUUID, err := uuid.Parse(req.WalletID)
	if err != nil {
		http.Error(w, `{"error":"invalid walletId UUID"}`, http.StatusBadRequest)
		return
	}

	out, err := h.wagerUseCase.Execute(r.Context(), usecase.ProcessWagerInput{
		ProviderID:                     req.ProviderID,
		ExternalTransactionID:          req.ExternalTransactionID,
		IdempotencyKey:                 idempotencyKey,
		PlayerID:                       playerUUID,
		WalletID:                       walletUUID,
		RoundID:                        req.RoundID,
		GameID:                         req.GameID,
		Kind:                           wager.Kind(req.Kind),
		Money:                          req.Money,
		ReferenceExternalTransactionID: req.ReferenceExternalTransactionID,
	})
	if err != nil {
		if errors.Is(err, wager.ErrIdempotencyConflict) {
			http.Error(w, `{"error":"idempotency key conflict: reused with conflicting payload"}`, http.StatusConflict)
			return
		}
		if errors.Is(err, wager.ErrOpeningNotAllowedExternal) ||
			errors.Is(err, wager.ErrInvalidKind) ||
			errors.Is(err, wager.ErrMissingReferenceForReversal) ||
			errors.Is(err, wager.ErrLossMustBeZero) ||
			errors.Is(err, wager.ErrZeroAmountNotAllowedForKind) {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		h.logger.Error("failed to process wager", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if out.Status == wager.StatusPendingReference {
		w.WriteHeader(http.StatusAccepted)
	} else if out.Status == wager.StatusRejected {
		w.WriteHeader(http.StatusUnprocessableEntity)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	_ = json.NewEncoder(w).Encode(out)
}

func (h *WagerHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	txIDStr := chi.URLParam(r, "transactionId")
	txID, err := uuid.Parse(txIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid transaction ID"}`, http.StatusBadRequest)
		return
	}

	tx, err := h.txRepo.FindByID(r.Context(), txID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			http.Error(w, `{"error":"transaction not found"}`, http.StatusNotFound)
			return
		}
		h.logger.Error("failed to get transaction by ID", "transactionId", txID, "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Provider isolation check
	authProvider := middleware.GetProviderID(r.Context())
	if authProvider != "" && authProvider != "SYSTEM" && authProvider != tx.ProviderID() {
		http.Error(w, `{"error":"forbidden: provider isolation violation"}`, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tx)
}

func (h *WagerHandler) GetByExternalID(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	externalID := chi.URLParam(r, "externalTransactionId")

	// Provider isolation
	authProvider := middleware.GetProviderID(r.Context())
	if authProvider != "" && authProvider != "SYSTEM" && authProvider != providerID {
		http.Error(w, `{"error":"forbidden: provider isolation violation"}`, http.StatusForbidden)
		return
	}

	tx, err := h.txRepo.FindByProviderAndExternalID(r.Context(), providerID, externalID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			http.Error(w, `{"error":"transaction not found"}`, http.StatusNotFound)
			return
		}
		h.logger.Error("failed to get transaction by external ID", "providerId", providerID, "externalId", externalID, "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tx)
}
