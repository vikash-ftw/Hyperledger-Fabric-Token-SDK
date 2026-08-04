package views

import (
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/view"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	"github.com/pkg/errors"
)

// AcceptCashView responds to both IssueCashView and TransferView: the
// receiving side is identical, so owner/main.go registers it against both.
type AcceptCashView struct{}

func (v *AcceptCashView) Call(context view.Context) (interface{}, error) {
	id, err := ttx.RespondRequestRecipientIdentity(context)
	if err != nil {
		return nil, errors.Wrap(err, "failed responding to recipient identity request")
	}

	tx, err := ttx.ReceiveTransaction(context)
	if err != nil {
		return nil, errors.Wrap(err, "failed receiving transaction")
	}

	// Accepting blindly would let a counterparty collect our endorsement on
	// a transaction that pays us nothing.
	outputs, err := tx.Outputs()
	if err != nil {
		return nil, errors.Wrapf(err, "failed getting outputs of tx [%s]", tx.ID())
	}
	mine := outputs.ByRecipient(id)
	if mine.Count() == 0 {
		return nil, errors.Errorf("tx [%s] contains no outputs for this recipient", tx.ID())
	}

	logger.Infof("accepting tx [%s] with [%d] output(s) for us", tx.ID(), mine.Count())

	if _, err := context.RunView(ttx.NewAcceptView(tx)); err != nil {
		return nil, errors.Wrapf(err, "failed accepting tx [%s]", tx.ID())
	}

	if _, err := context.RunView(ttx.NewFinalityView(tx)); err != nil {
		return nil, errors.Wrapf(err, "failed waiting for finality of tx [%s]", tx.ID())
	}

	return tx.ID(), nil
}
