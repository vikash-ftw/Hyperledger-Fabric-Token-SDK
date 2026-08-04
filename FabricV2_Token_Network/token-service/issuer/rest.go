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

type issueRequest struct {
	TokenType     string `json:"tokenType"`
	Quantity      uint64 `json:"quantity"`
	Recipient     string `json:"recipient"`
	RecipientNode string `json:"recipientNode"`
	Message       string `json:"message"`
}

func startREST(svc TokenService, addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /issue", handleIssue(svc))

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

func handleIssue(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req issueRequest
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
		case req.Recipient == "":
			writeErr(w, http.StatusBadRequest, "recipient is required")
			return
		case req.RecipientNode == "":
			writeErr(w, http.StatusBadRequest, "recipientNode is required")
			return
		}

		// Threaded into the view, so a client disconnect cancels the flow.
		txID, err := svc.Issue(r.Context(), req.TokenType, req.Quantity, req.Recipient, req.RecipientNode, req.Message)
		if err != nil {
			logger.Errorf("issue failed: %+v", err)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "issued " + itoa(req.Quantity) + " " + req.TokenType + " to " + req.Recipient,
			Payload: txID,
		})
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}

	return string(buf[i:])
}
