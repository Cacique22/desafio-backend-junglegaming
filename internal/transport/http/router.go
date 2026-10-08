package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/junglegaming/backend-challenge-go/internal/transport/http/handler"
	"github.com/junglegaming/backend-challenge-go/internal/transport/http/middleware"
)

func NewRouter(
	authMiddleware *middleware.AuthMiddleware,
	walletHandler *handler.WalletHandler,
	wagerHandler *handler.WagerHandler,
	healthHandler *handler.HealthHandler,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	// Public Health Check endpoints
	r.Get("/health/live", healthHandler.Live)
	r.Get("/health/ready", healthHandler.Ready)

	// Authenticated routes
	r.Group(func(protected chi.Router) {
		protected.Use(authMiddleware.Authenticate)

		// Internal administrative operations
		protected.Group(func(internal chi.Router) {
			internal.Use(authMiddleware.RequireInternal)
			internal.Post("/wallets", walletHandler.Create)
			internal.Get("/wallets/{walletId}", walletHandler.GetByID)
			internal.Get("/wallets/{walletId}/ledger", walletHandler.ListLedger)
			internal.Post("/wallets/{walletId}/reconciliation", walletHandler.Reconcile)
		})

		// Wagering transaction endpoints
		protected.Post("/wagering/transactions", wagerHandler.Process)
		protected.Get("/wagering/transactions/{transactionId}", wagerHandler.GetByID)
		protected.Get("/providers/{providerId}/wagering/transactions/{externalTransactionId}", wagerHandler.GetByExternalID)
	})

	return r
}
