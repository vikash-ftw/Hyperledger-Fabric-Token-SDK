package views

import (
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services/id"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/view"
	token1 "github.com/hyperledger-labs/fabric-token-sdk/token"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	token2 "github.com/hyperledger-labs/fabric-token-sdk/token/token"
	"github.com/pkg/errors"
)

// IssuerNodeName names the issuer node in fsc.endpoint.resolvers. A redeem is
// not a local burn: tokens return to the issuer, which must countersign, so
// the redeeming node needs to know which node to open a session to.
const IssuerNodeName = "issuer"

// Redeem is the request payload for burning tokens.
type Redeem struct {
	TokenType string
	Quantity  uint64
	Wallet    string
	// TokenIDs optionally pins the exact tokens to burn; empty lets the
	// selector choose.
	TokenIDs []*token2.ID
	Message  string
}

// RedeemView is the owner-initiated burn flow.
type RedeemView struct {
	*Redeem
}

func (v *RedeemView) Call(context view.Context) (interface{}, error) {
	idp, err := id.GetProvider(context)
	if err != nil {
		return "", errors.Wrap(err, "failed getting identity provider")
	}
	auditor := idp.Identity("auditor")

	tx, err := ttx.NewTransaction(context, nil, ttx.WithAuditor(auditor))
	if err != nil {
		return "", errors.Wrap(err, "failed creating redeem transaction")
	}

	if len(v.Message) > 0 {
		tx.SetApplicationMetadata("message", []byte(v.Message))
	}

	senderWallet := ttx.GetWallet(context, v.Wallet)
	if senderWallet == nil {
		return "", errors.Errorf("wallet [%s] not found", v.Wallet)
	}

	tms, err := token1.GetManagementService(context)
	if err != nil {
		return "", errors.Wrap(err, "failed getting token management service")
	}
	issuers := tms.PublicParametersManager().PublicParameters().Issuers()
	if len(issuers) == 0 {
		return "", errors.New("no issuer found in the public parameters")
	}

	// Both issuer options are required and must be passed together
	// (token/core/common/transfer.go, SelectIssuerForRedeem): the first says
	// which FSC node endorses, the second which issuer key from the public
	// parameters it signs for. Only the first fails with "issuer public params
	// public key not found in opts"; neither fails with "endpoint not found
	// for identity <empty>". This network's pp carry exactly one issuer.
	if err := tx.Redeem(
		senderWallet,
		token2.Type(v.TokenType),
		v.Quantity,
		token1.WithTokenIDs(v.TokenIDs...),
		ttx.WithFSCIssuerIdentity(idp.Identity(IssuerNodeName)),
		ttx.WithIssuerPublicParamsPublicKey(issuers[0]),
	); err != nil {
		return "", errors.Wrapf(err, "failed adding redeem action for [%d %s]", v.Quantity, v.TokenType)
	}

	logger.Infof("redeeming [%d %s] from [%s] in tx [%s]", v.Quantity, v.TokenType, v.Wallet, tx.ID())

	if _, err := context.RunView(ttx.NewCollectEndorsementsView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed collecting endorsements for tx [%s]", tx.ID())
	}

	if _, err := context.RunView(ttx.NewOrderingAndFinalityView(tx)); err != nil {
		return "", errors.Wrapf(err, "failed ordering tx [%s]", tx.ID())
	}

	return tx.ID(), nil
}

// IssuerRedeemAcceptView is the issuer's responder to RedeemView. Without it
// registered (issuer/main.go) every redeem hangs at endorsement collection,
// while mint and transfer keep working - easy to miss until the first burn.
type IssuerRedeemAcceptView struct{}

func (v *IssuerRedeemAcceptView) Call(context view.Context) (interface{}, error) {
	tx, err := ttx.ReceiveTransaction(context)
	if err != nil {
		return nil, errors.Wrap(err, "failed receiving redeem transaction")
	}

	logger.Infof("endorsing redeem tx [%s]", tx.ID())

	if _, err := context.RunView(ttx.NewEndorseView(tx)); err != nil {
		return nil, errors.Wrapf(err, "issuer failed endorsing redeem tx [%s]", tx.ID())
	}

	return nil, nil
}
