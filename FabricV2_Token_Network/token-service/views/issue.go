package views

import (
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/endpoint"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/id"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/view"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	token2 "github.com/hyperledger-labs/fabric-token-sdk/token/token"
	"github.com/pkg/errors"
)

// IssueCash is the request payload for a mint.
//
// TokenType stays a string rather than the SDK's token.Type because this
// struct is the REST/JSON contract; conversion happens at the SDK call.
type IssueCash struct {
	TokenType string
	Quantity  uint64
	Recipient string
	// RecipientNode is the FSC node hosting the wallet, as named in
	// fsc.endpoint.resolvers.
	RecipientNode string
	Message       string
}

// IssueCashView is the issuer-initiated mint flow.
type IssueCashView struct {
	*IssueCash
}

func (v *IssueCashView) Call(context view.Context) (interface{}, error) {
	// The wallet ID and the node hosting it are different identities.
	// Binding them is what lets RequestRecipientIdentity below reach the node.
	node := view.Identity(v.RecipientNode)
	rec := view.Identity(v.Recipient)

	eps := endpoint.GetService(context)
	if !eps.IsBoundTo(context.Context(), node, rec) {
		if err := eps.Bind(context.Context(), node, rec); err != nil {
			return "", errors.Wrapf(err, "failed binding recipient [%s] to node [%s]", v.Recipient, v.RecipientNode)
		}
	}

	recipient, err := ttx.RequestRecipientIdentity(context, rec)
	if err != nil {
		return "", errors.Wrapf(err, "failed getting recipient identity for [%s]", v.Recipient)
	}

	idp, err := id.GetProvider(context)
	if err != nil {
		return "", errors.Wrap(err, "failed getting identity provider")
	}
	auditor := idp.Identity("auditor")

	tx, err := ttx.NewTransaction(context, nil, ttx.WithAuditor(auditor))
	if err != nil {
		return "", errors.Wrap(err, "failed creating issue transaction")
	}

	if len(v.Message) > 0 {
		tx.SetApplicationMetadata("message", []byte(v.Message))
	}

	wallet := ttx.MyIssuerWallet(context)
	if wallet == nil {
		return "", errors.New("issuer wallet not found")
	}

	if err := tx.Issue(wallet, recipient, token2.Type(v.TokenType), v.Quantity); err != nil {
		return "", errors.Wrapf(err, "failed adding issue action for [%d %s]", v.Quantity, v.TokenType)
	}

	logger.Infof("issuing [%d %s] to [%s] in tx [%s]", v.Quantity, v.TokenType, v.Recipient, tx.ID())

	if _, err := context.RunView(ttx.NewCollectEndorsementsView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed collecting endorsements for tx [%s]", tx.ID())
	}

	if _, err := context.RunView(ttx.NewOrderingAndFinalityView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed ordering tx [%s]", tx.ID())
	}

	return tx.ID(), nil
}
