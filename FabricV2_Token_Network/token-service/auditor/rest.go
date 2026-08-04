package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type response struct {
	Message string      `json:"message"`
	Payload interface{} `json:"payload,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, response{Message: msg})
}

func startREST(svc TokenService, addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /accounts/{wallet}", handleBalance(svc))
	mux.HandleFunc("GET /accounts/{wallet}/transactions", handleHistory(svc))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Infof("REST API listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("REST server error: %v", err)
		}
	}()

	return srv
}

func shutdownREST(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("REST shutdown error: %v", err)
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Message: "ok"})
}

func handleBalance(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wallet := r.PathValue("wallet")
		if wallet == "" {
			writeErr(w, http.StatusBadRequest, "wallet is required")
			return
		}

		tokenType := r.URL.Query().Get("tokenType")
		if tokenType == "" {
			writeErr(w, http.StatusBadRequest, "tokenType query parameter is required")
			return
		}

		balances, err := svc.GetBalance(r.Context(), wallet, tokenType)
		if err != nil {
			logger.Errorf("audited balance query failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "got audited balances for " + wallet,
			Payload: balances,
		})
	}
}

func handleHistory(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wallet := r.PathValue("wallet")
		if wallet == "" {
			writeErr(w, http.StatusBadRequest, "wallet is required")
			return
		}

		txs, err := svc.GetHistory(r.Context(), wallet)
		if err != nil {
			logger.Errorf("audited history query failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "got audited transaction history for " + wallet,
			Payload: txs,
		})
	}
}
