package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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

type transferRequest struct {
	TokenType     string `json:"tokenType"`
	Quantity      uint64 `json:"quantity"`
	Sender        string `json:"sender"`
	Recipient     string `json:"recipient"`
	RecipientNode string `json:"recipientNode"`
	Message       string `json:"message"`
}

type redeemRequest struct {
	TokenType string `json:"tokenType"`
	Quantity  uint64 `json:"quantity"`
	Wallet    string `json:"wallet"`
	Message   string `json:"message"`
}

// The caller supplies the handle; this node does not generate it. Handles are
// permanently burned once registered, so the caller must never reissue one.
type registerWalletRequest struct {
	WalletID string `json:"walletId"`
}

func startREST(svc TokenService, addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /register", handleRegisterWallet(svc))
	mux.HandleFunc("POST /transfer", handleTransfer(svc))
	mux.HandleFunc("POST /lock", handleLock(svc))
	mux.HandleFunc("POST /confirm", handleConfirm(svc))
	mux.HandleFunc("POST /cancel", handleCancel(svc))
	mux.HandleFunc("POST /redeem", handleRedeem(svc))
	mux.HandleFunc("GET /accounts", handleAllBalances(svc))
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

func handleRegisterWallet(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerWalletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		// Validate before touching the CA, so a bad handle costs nothing.
		if err := ValidateWalletID(req.WalletID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}

		walletID, err := svc.RegisterWallet(r.Context(), req.WalletID)
		if err != nil {
			// A duplicate handle is the caller's mistake, not a server fault.
			if strings.Contains(err.Error(), "already registered") {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			logger.Errorf("wallet registration failed for [%s]: %+v", req.WalletID, err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "wallet " + walletID + " registered",
			Payload: walletID,
		})
	}
}

func handleTransfer(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req transferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		switch {
		case req.TokenType == "":
			writeErr(w, http.StatusBadRequest, "tokenType is required")
			return
		case req.Quantity == 0:
			writeErr(w, http.StatusBadRequest, "quantity must be greater than zero")
			return
		case req.Sender == "":
			writeErr(w, http.StatusBadRequest, "sender is required")
			return
		case req.Recipient == "":
			writeErr(w, http.StatusBadRequest, "recipient is required")
			return
		}

		txID, err := svc.TransferTokens(r.Context(), req.TokenType, req.Quantity, req.Sender, req.Recipient, req.RecipientNode, req.Message)
		if err != nil {
			logger.Errorf("transfer failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: req.Sender + " transferred " + strconv.FormatUint(req.Quantity, 10) + " " + req.TokenType + " to " + req.Recipient,
			Payload: txID,
		})
	}
}

func handleRedeem(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req redeemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		switch {
		case req.TokenType == "":
			writeErr(w, http.StatusBadRequest, "tokenType is required")
			return
		case req.Quantity == 0:
			writeErr(w, http.StatusBadRequest, "quantity must be greater than zero")
			return
		case req.Wallet == "":
			writeErr(w, http.StatusBadRequest, "wallet is required")
			return
		}

		txID, err := svc.RedeemTokens(r.Context(), req.TokenType, req.Quantity, req.Wallet, req.Message)
		if err != nil {
			logger.Errorf("redeem failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: req.Wallet + " redeemed " + strconv.FormatUint(req.Quantity, 10) + " " + req.TokenType,
			Payload: txID,
		})
	}
}

func handleBalance(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wallet := r.PathValue("wallet")
		if wallet == "" {
			writeErr(w, http.StatusBadRequest, "wallet is required")
			return
		}

		balances, err := svc.GetBalance(r.Context(), wallet, r.URL.Query().Get("tokenType"))
		if err != nil {
			logger.Errorf("balance query failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "got balances for " + wallet,
			Payload: balances,
		})
	}
}

func handleAllBalances(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := svc.GetAllBalances(r.Context())
		if err != nil {
			logger.Errorf("all-balances query failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "got balances for all owner wallets",
			Payload: all,
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
			logger.Errorf("history query failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "got transaction history for " + wallet,
			Payload: txs,
		})
	}
}
