package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/pkg/errors"
)

type lockRequest struct {
	OrderID   string `json:"orderId"`
	TokenType string `json:"tokenType"`
	Quantity  uint64 `json:"quantity"`
	Sender    string `json:"sender"`
	// ListingID is recorded but not enforced yet.
	ListingID string `json:"listingId"`
}

type confirmRequest struct {
	OrderID   string `json:"orderId"`
	Recipient string `json:"recipient"`
}

type cancelRequest struct {
	OrderID string `json:"orderId"`
}

// The sentinels are caller mistakes, so they must not read as server faults -
// that is what lets a client tell "I sent this twice" from "the ledger is broken".
func writeOrderErr(w http.ResponseWriter, action string, err error) {
	switch {
	case errors.Is(err, errOrderExists),
		errors.Is(err, errOrderNotFound),
		errors.Is(err, errOrderNotLocked):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		logger.Errorf("%s failed: %+v", action, err)
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

func handleLock(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req lockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		if err := ValidateOrderID(req.OrderID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
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
		case req.Sender == EscrowWallet:
			// Would create an order that can never be meaningfully cancelled.
			writeErr(w, http.StatusBadRequest, "sender cannot be the escrow wallet")
			return
		}

		txID, err := svc.LockTokens(r.Context(), req.OrderID, req.TokenType, req.Quantity, req.Sender, req.ListingID)
		if err != nil {
			writeOrderErr(w, "lock", err)
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "locked " + strconv.FormatUint(req.Quantity, 10) + " " + req.TokenType +
				" from " + req.Sender + " for order " + req.OrderID,
			Payload: txID,
		})
	}
}

func handleConfirm(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req confirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		if err := ValidateOrderID(req.OrderID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Recipient == "" {
			writeErr(w, http.StatusBadRequest, "recipient is required")
			return
		}

		txID, err := svc.ConfirmOrder(r.Context(), req.OrderID, req.Recipient)
		if err != nil {
			writeOrderErr(w, "confirm", err)
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "confirmed order " + req.OrderID + " to " + req.Recipient,
			Payload: txID,
		})
	}
}

func handleCancel(svc TokenService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req cancelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		if err := ValidateOrderID(req.OrderID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}

		// No recipient parameter by design: a caller must not be able to
		// redirect an unwind to an arbitrary destination.
		txID, err := svc.CancelOrder(r.Context(), req.OrderID)
		if err != nil {
			writeOrderErr(w, "cancel", err)
			return
		}

		writeJSON(w, http.StatusOK, response{
			Message: "cancelled order " + req.OrderID,
			Payload: txID,
		})
	}
}
