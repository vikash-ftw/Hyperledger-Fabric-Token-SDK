package main

import (
	"context"

	"github.com/hyperledger-labs/fabric-smart-client/platform/common/utils/collections/iterators"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/storage/driver/sql/query/pagination"
	token1 "github.com/hyperledger-labs/fabric-token-sdk/token"
	dbdriver "github.com/hyperledger-labs/fabric-token-sdk/token/services/storage/db/driver"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	ttxdep "github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx/dep/db"
	token2 "github.com/hyperledger-labs/fabric-token-sdk/token/token"
	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
)

// TokenService answers the same questions as the owner but from the auditor's
// OWN database, populated by AuditView as it countersigns each transaction -
// an independent check rather than a second read of the same store.
type TokenService struct {
	FSC services.Provider
}

// Shared with the owner so the two nodes cannot describe the same transaction
// differently.
type TransactionHistoryItem = views.TransactionHistoryItem

// ttx.MyAuditorWallet needs a view.Context an HTTP handler does not have, but
// it only wraps the WalletManager and ttx.NewAuditor takes a plain
// ServiceProvider, so this path works outside a view.
func (s TokenService) auditor(ctx context.Context) (*ttx.Auditor, error) {
	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting token management service")
	}

	// "" selects the default auditor wallet.
	w, err := tms.WalletManager().AuditorWallet(ctx, "")
	if err != nil {
		return nil, errors.Wrap(err, "failed getting default auditor wallet")
	}

	a, err := ttx.NewAuditor(s.FSC, w)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting auditor service")
	}

	return a, nil
}

// tokenType is required: the holdings filter is built per type, so unlike the
// owner's endpoint there is no "all types" form.
func (s TokenService) GetBalance(ctx context.Context, wallet, tokenType string) (map[string]int64, error) {
	if tokenType == "" {
		return nil, errors.New("tokenType is required for auditor balance queries")
	}

	a, err := s.auditor(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := a.NewHoldingsFilter().
		ByEnrollmentId(wallet).
		ByType(token2.Type(tokenType)).
		Execute(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed retrieving holdings for [%s][%s]", wallet, tokenType)
	}

	return map[string]int64{tokenType: filter.Sum().Int64()}, nil
}

// GetHistory returns every audited transaction involving a wallet.
func (s TokenService) GetHistory(ctx context.Context, wallet string) ([]TransactionHistoryItem, error) {
	a, err := s.auditor(ctx)
	if err != nil {
		return nil, err
	}

	it, err := a.Transactions(ctx, ttxdep.QueryTransactionsParams{
		SenderWallet:    wallet,
		RecipientWallet: wallet,
	}, pagination.None())
	if err != nil {
		return nil, errors.Wrapf(err, "failed querying audited transactions for [%s]", wallet)
	}

	records, err := iterators.ReadAllPointers(it.Items)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading audited transactions for [%s]", wallet)
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
