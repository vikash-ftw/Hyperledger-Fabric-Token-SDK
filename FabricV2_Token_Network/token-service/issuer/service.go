package main

import (
	"context"

	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services"
	viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view/services/view"
	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
)

// TokenService is the business layer behind the REST handlers.
type TokenService struct {
	FSC services.Provider
}

// Issue runs IssueCashView. Minting is a multi-party interactive flow, so it
// must run as a view rather than a direct SDK call; this blocks until finality.
func (s TokenService) Issue(ctx context.Context, tokenType string, quantity uint64, recipient, recipientNode, message string) (string, error) {
	manager, err := viewregistry.GetManager(s.FSC)
	if err != nil {
		return "", errors.Wrap(err, "failed getting view manager")
	}

	res, err := manager.InitiateView(ctx, &views.IssueCashView{
		IssueCash: &views.IssueCash{
			TokenType:     tokenType,
			Quantity:      quantity,
			Recipient:     recipient,
			RecipientNode: recipientNode,
			Message:       message,
		},
	})
	if err != nil {
		return "", errors.Wrapf(err, "failed issuing [%d %s] to [%s]", quantity, tokenType, recipient)
	}

	txID, _ := res.(string)

	return txID, nil
}
