package views

import "time"

// The operation a transaction performed, in this API's vocabulary rather than
// the SDK's. The SDK's ActionType collapses /transfer, /lock, /confirm and
// /cancel into one "transfer" value, losing the distinction a caller reading
// history actually needs.
const (
	OperationIssue    = "ISSUE"
	OperationTransfer = "TRANSFER"
	OperationLock     = "LOCK"
	OperationConfirm  = "CONFIRM"
	OperationCancel   = "CANCEL"
	OperationRedeem   = "REDEEM"
)

const (
	// EscrowWallet holds locked tokens between /lock and /confirm|/cancel.
	EscrowWallet = "escrow"

	// OrderIDMetadataKey tags every leg of a DvP order with its order id.
	OrderIDMetadataKey = "orderId"

	// DvPActionMetadataKey records which DvP operation produced the transaction.
	//
	// Written at transfer time rather than derived at read time so the owner and
	// the auditor reach the same answer: an order's outcome otherwise lives only
	// in the owner's app_order_locks table, which the auditor cannot see and must
	// not be given if it is to stay an independent view.
	//
	// Application metadata is the only channel a caller cannot forge - /transfer
	// passes nil metadata and the REST layer exposes no metadata field, so
	// transferTagged is the sole writer of these keys. The `message` field is
	// caller-supplied, which is why it is never parsed for this.
	DvPActionMetadataKey = "dvpAction"
)

// The DvP values written into DvPActionMetadataKey. Lower-case because dvp.go
// reuses the same string for its log lines and its "dvp-<action>:<orderId>"
// message tag; the upper-case Operation* form is this API's presentation of it.
// Exported so the writer and the reader cannot disagree over a string literal.
const (
	DvPActionLock    = "lock"
	DvPActionConfirm = "confirm"
	DvPActionCancel  = "cancel"
)

// The SDK's driver.ActionType values.
const (
	ActionTypeIssue = iota
	ActionTypeTransfer
	ActionTypeRedeem
)

type TransactionHistoryItem struct {
	TxID          string    `json:"txId"`
	OperationType string    `json:"operationType"`
	OrderID       string    `json:"orderId,omitempty"`
	Sender        string    `json:"sender"`
	Recipient     string    `json:"recipient"`
	TokenType     string    `json:"tokenType"`
	Amount        int64     `json:"amount"`
	Timestamp     time.Time `json:"timestamp"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
}

// TxClassification is what a transaction knows about itself that a single
// history row does not carry.
type TxClassification struct {
	ActionType int
	DvPAction  string
}

// ClassifyOperations fills OperationType on every item, given per-transaction
// facts keyed by transaction id.
//
// The decision is made once per TRANSACTION, not per row: fabtoken is UTXO-based,
// so spending a large token to send a small amount also emits a change output
// back to the sender. That row names neither escrow nor a counterparty, so a
// row-local rule would label the change half of a lock as a plain transfer.
func ClassifyOperations(items []TransactionHistoryItem, byTx map[string]TxClassification) {
	operations := make(map[string]string, len(items))
	for _, item := range items {
		if _, done := operations[item.TxID]; done {
			continue
		}
		operations[item.TxID] = classify(item, byTx[item.TxID])
	}

	for i := range items {
		items[i].OperationType = operations[items[i].TxID]
	}
}

func classify(item TransactionHistoryItem, tx TxClassification) string {
	switch tx.ActionType {
	case ActionTypeIssue:
		return OperationIssue
	case ActionTypeRedeem:
		return OperationRedeem
	}

	switch {
	case item.OrderID == "":
		return OperationTransfer
	case tx.DvPAction == DvPActionLock:
		return OperationLock
	case tx.DvPAction == DvPActionConfirm:
		return OperationConfirm
	case tx.DvPAction == DvPActionCancel:
		return OperationCancel
	}

	// An order leg with no recorded action means this node wrote the transaction
	// without stamping it, so report the ledger-level truth rather than guessing
	// which half of the order it was.
	logger.Errorf("transaction [%s] carries order [%s] but no usable %s metadata (got %q)",
		item.TxID, item.OrderID, DvPActionMetadataKey, tx.DvPAction)

	return OperationTransfer
}
