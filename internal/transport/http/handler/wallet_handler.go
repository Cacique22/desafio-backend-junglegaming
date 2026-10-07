package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/junglegaming/backend-challenge-go/internal/domain/money"
	"github.com/junglegaming/backend-challenge-go/internal/infra/repository"
	"github.com/junglegaming/backend-challenge-go/internal/usecase"
)

type WalletHandler struct {
	walletUseCase *usecase.WalletUseCase
	ledgerRepo    *repository.LedgerRepository
}

func NewWalletHandler(walletUseCase *usecase.WalletUseCase, ledgerRepo *repository.LedgerRepository) *WalletHandler {
	return &WalletHandler{
		walletUseCase: walletUseCase,
		ledgerRepo:    ledgerRepo,
	}
}

type CreateWalletRequest struct {
	PlayerID       string      `json:"playerId"`
	InitialBalance money.Money `json:"initialBalance"`
}

func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	playerUUID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		http.Error(w, `{"error":"invalid playerId UUID"}`, http.StatusBadRequest)
		return
	}

	out, err := h.walletUseCase.CreateWallet(r.Context(), usecase.CreateWalletInput{
		PlayerID:       playerUUID,
		InitialBalance: req.InitialBalance,
	})
	if err != nil {
		if errors.Is(err, usecase.ErrWalletAlreadyExists) {
			http.Error(w, `{"error":"wallet already exists for player and currency"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

func (h *WalletHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "walletId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid wallet ID"}`, http.StatusBadRequest)
		return
	}

	out, err := h.walletUseCase.GetWallet(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrWalletNotFound) {
			http.Error(w, `{"error":"wallet not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *WalletHandler) ListLedger(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "walletId")
	walletID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid wallet ID"}`, http.StatusBadRequest)
		return
	}

	limit := 50
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if l, err := strconv.Atoi(limitParam); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	cursor := r.URL.Query().Get("cursor")

	items, nextCursor, err := h.ledgerRepo.ListByWallet(r.Context(), walletID, limit, cursor)
	if err != nil {
		http.Error(w, `{"error":"failed to list ledger"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"items":      items,
		"nextCursor": nextCursor,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *WalletHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "walletId")
	walletID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid wallet ID"}`, http.StatusBadRequest)
		return
	}

	out, err := h.walletUseCase.ReconcileWallet(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, repository.ErrWalletNotFound) {
			http.Error(w, `{"error":"wallet not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
