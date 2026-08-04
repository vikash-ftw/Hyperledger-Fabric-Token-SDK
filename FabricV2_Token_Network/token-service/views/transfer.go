package views

import (
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/endpoint"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/id"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/view"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	token2 "github.com/hyperledger-labs/fabric-token-sdk/token/token"
	"github.com/pkg/errors"
)

// Transfer is the request payload for moving tokens between wallets.
type Transfer struct {
	TokenType     string
	Quantity      uint64
	Wallet        string
	Recipient     string
	RecipientNode string
	Message       string
	// Metadata tags extra application metadata onto the transaction, used by
	// the DvP endpoints to mark every leg of an order with its orderId. A nil
	// map produces exactly what a plain /transfer would.
	//
	// These keys are readable per-transaction but NOT server-side queryable -
	// QueryTransactionsParams has no metadata filter - so never use them for
	// lookups.
	Metadata map[string]string
}

// TransferView is the owner-initiated transfer flow.
type TransferView struct {
	*Transfer
}

func (v *TransferView) Call(context view.Context) (interface{}, error) {
	var recipient view.Identity

	// A recipient wallet on this node can be resolved locally, skipping the
	// network round trip; otherwise bind it to its host node and ask.
	if w := ttx.GetWallet(context, v.Recipient); w != nil {
		var err error
		recipient, err = w.GetRecipientIdentity(context.Context())
		if err != nil {
			return "", errors.Wrapf(err, "failed getting local recipient identity for [%s]", v.Recipient)
		}
	} else {
		node := view.Identity(v.RecipientNode)
		rec := view.Identity(v.Recipient)

		eps := endpoint.GetService(context)
		if !eps.IsBoundTo(context.Context(), node, rec) {
			if err := eps.Bind(context.Context(), node, rec); err != nil {
				return "", errors.Wrapf(err, "failed binding recipient [%s] to node [%s]", v.Recipient, v.RecipientNode)
			}
		}

		var err error
		recipient, err = ttx.RequestRecipientIdentity(context, rec)
		if err != nil {
			return "", errors.Wrapf(err, "failed getting recipient identity for [%s]", v.Recipient)
		}
	}

	idp, err := id.GetProvider(context)
	if err != nil {
		return "", errors.Wrap(err, "failed getting identity provider")
	}
	auditor := idp.Identity("auditor")

	tx, err := ttx.NewTransaction(context, nil, ttx.WithAuditor(auditor))
	if err != nil {
		return "", errors.Wrap(err, "failed creating transfer transaction")
	}

	if len(v.Message) > 0 {
		tx.SetApplicationMetadata("message", []byte(v.Message))
	}
	for k, val := range v.Metadata {
		// "message" is set above; do not let extra metadata overwrite it.
		if k == "" || k == "message" {
			continue
		}
		tx.SetApplicationMetadata(k, []byte(val))
	}

	senderWallet := ttx.GetWallet(context, v.Wallet)
	if senderWallet == nil {
		return "", errors.Errorf("sender wallet [%s] not found", v.Wallet)
	}

	if err := tx.Transfer(
		senderWallet,
		token2.Type(v.TokenType),
		[]uint64{v.Quantity},
		[]view.Identity{recipient},
	); err != nil {
		return "", errors.Wrapf(err, "failed adding transfer action for [%d %s]", v.Quantity, v.TokenType)
	}

	logger.Infof("transferring [%d %s] from [%s] to [%s] in tx [%s]",
		v.Quantity, v.TokenType, v.Wallet, v.Recipient, tx.ID())

	if _, err := context.RunView(ttx.NewCollectEndorsementsView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed collecting endorsements for tx [%s]", tx.ID())
	}

	if _, err := context.RunView(ttx.NewOrderingAndFinalityView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed ordering tx [%s]", tx.ID())
	}

	return tx.ID(), nil
}
