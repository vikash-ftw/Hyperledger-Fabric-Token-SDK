package main

import (
	"context"
	"regexp"

	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
)

// EscrowWallet is an ordinary owner wallet; the escrow property comes from this
// service refusing to move its tokens except via a matching order, not from the
// ledger. It lives in views because the auditor classifies history too and
// cannot import this package.
const EscrowWallet = views.EscrowWallet

// All three map to HTTP 400: each is a caller mistake, not a server fault.
var (
	errOrderExists    = errors.New("orderId has already been used")
	errOrderNotFound  = errors.New("orderId does not correspond to a prior lock")
	errOrderNotLocked = errors.New("order is not in the locked state (already confirmed or cancelled)")
)

// Conservative because orderIds are caller-supplied and reach application
// metadata and SQL.
var orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func ValidateOrderID(orderID string) error {
	if orderID == "" {
		return errors.New("orderId is required")
	}
	if !orderIDPattern.MatchString(orderID) {
		return errors.New("orderId must be 1-128 chars of [A-Za-z0-9._:-] and start alphanumeric")
	}

	return nil
}

// A struct so adding fields later does not change call sites.
type authorityRequest struct {
	Action    string // "confirm" or "cancel"
	OrderID   string
	Order     OrderLock
	Recipient string // confirm only
}

// requireAuthority is the single authorization decision point for settling an
// order, and is INTENTIONALLY A NO-OP for now. It exists so that enabling auth
// becomes a change to this one function rather than a refactor of the endpoints.
//
// TODO(auth): implement. Until then ANY caller who can reach the REST port can
// settle or cancel ANY locked order.
func requireAuthority(_ context.Context, _ authorityRequest) error {
	return nil
}

// LockTokens moves tokens from a sender wallet into escrow, claiming orderId
// for the lifetime of the order.
//
// The claim precedes the transfer so two concurrent /lock calls with the same
// orderId cannot both reach the ledger; the loser of the primary-key race is
// rejected.
func (s TokenService) LockTokens(ctx context.Context, orderID, tokenType string, quantity uint64, sender, listingID string) (string, error) {
	if err := ValidateOrderID(orderID); err != nil {
		return "", err
	}
	if s.orders == nil {
		return "", errors.New("order store is not available")
	}

	if err := s.orders.Claim(ctx, OrderLock{
		OrderID:   orderID,
		TokenType: tokenType,
		Quantity:  quantity,
		Sender:    sender,
		ListingID: listingID,
	}); err != nil {
		return "", err
	}

	txID, err := s.transferTagged(ctx, views.DvPActionLock, orderID, tokenType, quantity, sender, EscrowWallet)
	if err != nil {
		// Tokens never moved, so free the orderId for retry rather than
		// leaving it wedged in `locked`.
		if abErr := s.orders.Abandon(ctx, orderID); abErr != nil {
			logger.Errorf("failed abandoning order [%s] after lock failure: %v", orderID, abErr)
		}

		return "", err
	}

	if err := s.orders.SetLockTxID(ctx, orderID, txID); err != nil {
		// Lock is on-chain and the order is `locked`; only bookkeeping failed,
		// and settlement still works.
		logger.Errorf("lock for order [%s] committed as [%s] but recording the txid failed: %v", orderID, txID, err)
	}

	logger.Infof("locked [%d %s] from [%s] for order [%s] in tx [%s]", quantity, tokenType, sender, orderID, txID)

	return txID, nil
}

// ConfirmOrder settles a locked order: escrow -> recipient.
func (s TokenService) ConfirmOrder(ctx context.Context, orderID, recipient string) (string, error) {
	return s.settle(ctx, orderID, views.DvPActionConfirm, recipient)
}

// CancelOrder unwinds a locked order back to the sender recorded at lock time.
// The caller does not choose the destination.
func (s TokenService) CancelOrder(ctx context.Context, orderID string) (string, error) {
	return s.settle(ctx, orderID, views.DvPActionCancel, "")
}

func (s TokenService) settle(ctx context.Context, orderID, action, recipient string) (string, error) {
	if err := ValidateOrderID(orderID); err != nil {
		return "", err
	}
	if s.orders == nil {
		return "", errors.New("order store is not available")
	}

	order, err := s.orders.Get(ctx, orderID)
	if err != nil {
		return "", err
	}
	if order.Status != OrderStatusLocked {
		return "", errOrderNotLocked
	}

	if err := requireAuthority(ctx, authorityRequest{
		Action:    action,
		OrderID:   orderID,
		Order:     order,
		Recipient: recipient,
	}); err != nil {
		return "", err
	}

	dest := recipient
	newStatus := OrderStatusConfirmed
	if action == views.DvPActionCancel {
		dest = order.Sender
		newStatus = OrderStatusCancelled
	}
	if dest == "" {
		return "", errors.New("recipient is required")
	}

	// Flip status BEFORE moving tokens: Release's `AND status = 'locked'` guard
	// makes a concurrent second confirm/cancel lose here and never reach the
	// ledger. A failed transfer is rolled back below, so retry stays possible.
	if err := s.orders.Release(ctx, orderID, newStatus, ""); err != nil {
		return "", err
	}

	txID, err := s.transferTagged(ctx, action, orderID, order.TokenType, order.Quantity, EscrowWallet, dest)
	if err != nil {
		if rbErr := s.rollbackToLocked(ctx, orderID); rbErr != nil {
			logger.Errorf("order [%s] %s failed AND could not be rolled back to locked: %v (original: %v)",
				orderID, action, rbErr, err)
		}

		return "", err
	}

	if err := s.orders.setFinalTxID(ctx, orderID, txID); err != nil {
		logger.Errorf("order [%s] %s committed as [%s] but recording the txid failed: %v", orderID, action, txID, err)
	}

	logger.Infof("%sed order [%s]: [%d %s] escrow -> [%s] in tx [%s]",
		action, orderID, order.Quantity, order.TokenType, dest, txID)

	return txID, nil
}

// rollbackToLocked returns an order to `locked` after a failed settlement so
// escrowed tokens are not stranded.
func (s TokenService) rollbackToLocked(ctx context.Context, orderID string) error {
	_, err := s.orders.db.ExecContext(ctx,
		`UPDATE app_order_locks SET status = $2, updated_at = NOW() WHERE order_id = $1`,
		orderID, OrderStatusLocked)

	return errors.Wrap(err, "failed rolling order back to locked")
}

// recipientNode is always "owner": every wallet a DvP order settles between
// lives on this node.
//
// The action reaches history through metadata, not through the message: message
// is caller-supplied on /transfer, so a reader that trusted it could be told a
// plain transfer was a confirm. Both are derived from the same argument here so
// the human-readable tag and the machine-readable key cannot drift.
func (s TokenService) transferTagged(ctx context.Context, action, orderID, tokenType string, quantity uint64, sender, recipient string) (string, error) {
	return s.transferWithMetadata(ctx, tokenType, quantity, sender, recipient, "owner",
		"dvp-"+action+":"+orderID,
		map[string]string{
			views.OrderIDMetadataKey:   orderID,
			views.DvPActionMetadataKey: action,
		})
}
