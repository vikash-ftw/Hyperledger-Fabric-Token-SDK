package main

import (
	"context"
	"strconv"

	"github.com/hyperledger-labs/fabric-smart-client/platform/common/utils/collections/iterators"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/storage/driver/sql/query/pagination"
	viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view/services/view"
	token1 "github.com/hyperledger-labs/fabric-token-sdk/token"
	dbdriver "github.com/hyperledger-labs/fabric-token-sdk/token/services/storage/db/driver"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	ttxdep "github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx/dep/db"
	token2 "github.com/hyperledger-labs/fabric-token-sdk/token/token"
	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
)

// TokenService is the business layer behind the owner's REST handlers.
type TokenService struct {
	FSC services.Provider

	// orders tracks DvP order state for /lock, /confirm and /cancel.
	//
	// A separate store rather than a ledger query, for two reasons verified
	// against v0.10.1: application metadata is not queryable
	// (QueryTransactionsParams has no metadata filter, so finding "the lock for
	// order X" means paging all history), and the ledger has no concept of
	// order state at all. The table also makes order_id a primary key, so a
	// duplicate /lock loses the insert instead of double-spending.
	//
	// nil is tolerated so the node still serves everything else if the store
	// cannot be opened; the DvP endpoints then fail cleanly.
	orders *orderStore
}

// Shared with the auditor so the two nodes cannot describe the same transaction
// differently.
type TransactionHistoryItem = views.TransactionHistoryItem

func (s TokenService) TransferTokens(ctx context.Context, tokenType string, quantity uint64, sender, recipient, recipientNode, message string) (string, error) {
	return s.transferWithMetadata(ctx, tokenType, quantity, sender, recipient, recipientNode, message, nil)
}

// Passing nil metadata is byte-for-byte equivalent to a plain /transfer, which
// is why that endpoint is unaffected by the DvP work.
func (s TokenService) transferWithMetadata(ctx context.Context, tokenType string, quantity uint64, sender, recipient, recipientNode, message string, metadata map[string]string) (string, error) {
	manager, err := viewregistry.GetManager(s.FSC)
	if err != nil {
		return "", errors.Wrap(err, "failed getting view manager")
	}

	res, err := manager.InitiateView(ctx, &views.TransferView{
		Transfer: &views.Transfer{
			TokenType:     tokenType,
			Quantity:      quantity,
			Wallet:        sender,
			Recipient:     recipient,
			RecipientNode: recipientNode,
			Message:       message,
			Metadata:      metadata,
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed transferring [%d %s] from [%s] to [%s]", quantity, tokenType, sender, recipient)
	}

	txID, _ := res.(string)

	return txID, nil
}

func (s TokenService) RedeemTokens(ctx context.Context, tokenType string, quantity uint64, wallet, message string) (string, error) {
	manager, err := viewregistry.GetManager(s.FSC)
	if err != nil {
		return "", errors.Wrap(err, "failed getting view manager")
	}

	res, err := manager.InitiateView(ctx, &views.RedeemView{
		Redeem: &views.Redeem{
			TokenType: tokenType,
			Quantity:  quantity,
			Wallet:    wallet,
			Message:   message,
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed redeeming [%d %s] from [%s]", quantity, tokenType, wallet)
	}

	txID, _ := res.(string)

	return txID, nil
}

// GetBalance sums a wallet's unspent tokens per type; an empty tokenType
// returns every type held.
//
// No view is initiated: ttx.GetWallet needs a view.Context an HTTP handler does
// not have, but it only wraps a WalletManager API that takes a plain context.
func (s TokenService) GetBalance(ctx context.Context, wallet, tokenType string) (map[string]int64, error) {
	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting token management service")
	}

	w, err := tms.WalletManager().OwnerWallet(ctx, wallet)
	if err != nil {
		return nil, errors.Wrapf(err, "wallet [%s] not found", wallet)
	}

	opts := []token1.ListTokensOption{token1.WithContext(ctx)}
	if tokenType != "" {
		opts = append(opts, token1.WithType(token2.Type(tokenType)))
	}

	unspent, err := w.ListUnspentTokens(opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed listing unspent tokens for [%s]", wallet)
	}

	// Summed per type rather than via Balance(), which collapses to a single
	// number and cannot answer the all-types query.
	balances := map[string]int64{}
	for _, tok := range unspent.Tokens {
		q, err := strconv.ParseInt(tok.Quantity, 10, 64)
		if err != nil {
			// Some driver versions hex-encode quantities ("0x...").
			parsed, perr := strconv.ParseInt(tok.Quantity, 0, 64)
			if perr != nil {
				return nil, errors.Wrapf(err, "failed parsing quantity [%s] of token [%s]", tok.Quantity, tok.Id)
			}
			q = parsed
		}
		balances[string(tok.Type)] += q
	}

	return balances, nil
}

func (s TokenService) GetAllBalances(ctx context.Context) (map[string]map[string]int64, error) {
	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting token management service")
	}

	walletIDs, err := tms.WalletManager().OwnerWalletIDs(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed listing owner wallet ids")
	}

	all := make(map[string]map[string]int64, len(walletIDs))
	for _, id := range walletIDs {
		b, err := s.GetBalance(ctx, id, "")
		if err != nil {
			return nil, err
		}
		all[id] = b
	}

	return all, nil
}

// GetHistory returns the transactions this node recorded for a wallet, as
// sender or recipient.
func (s TokenService) GetHistory(ctx context.Context, wallet string) ([]TransactionHistoryItem, error) {
	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting token management service")
	}

	owner := ttx.NewOwner(s.FSC, tms)

	// pagination.None() means everything in one page.
	it, err := owner.Transactions(ctx, ttxdep.QueryTransactionsParams{
		SenderWallet:    wallet,
		RecipientWallet: wallet,
	}, pagination.None())
	if err != nil {
		return nil, errors.Wrapf(err, "failed querying transactions for [%s]", wallet)
	}

	records, err := iterators.ReadAllPointers(it.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading transactions for [%s]", wallet)
	}

	out := make([]TransactionHistoryItem, 0, len(records))
	byTx := make(map[string]views.TxClassification, len(records))
	for _, tx := range records {
		item := TransactionHistoryItem{
			TxID:      tx.TxID,
			Sender:    tx.SenderEID,
			Recipient: tx.RecipientEID,
			TokenType: string(tx.TokenType),
			Timestamp: tx.Timestamp.UTC(),
			// TxStatus is an int, so string(tx.Status) would yield a garbage
			// rune rather than the status name.
			Status: dbdriver.TxStatusMessage[tx.Status],
		}
		if tx.Amount != nil {
			item.Amount = tx.Amount.Int64()
		}
		if m, ok := tx.ApplicationMetadata["message"]; ok && len(m) > 0 {
			item.Message = string(m)
		}
		item.OrderID = string(tx.ApplicationMetadata[views.OrderIDMetadataKey])
		byTx[tx.TxID] = views.TxClassification{
			ActionType: int(tx.ActionType),
			DvPAction:  string(tx.ApplicationMetadata[views.DvPActionMetadataKey]),
		}
		out = append(out, item)
	}
	views.ClassifyOperations(out, byTx)

	return out, nil
}
