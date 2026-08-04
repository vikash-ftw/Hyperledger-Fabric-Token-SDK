package views

import (
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/view"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	"github.com/pkg/errors"
)

// AuditView responds to ttx.AuditingViewInitiator, which the SDK triggers
// during endorsement collection for any transaction built with ttx.WithAuditor.
//
// Every transaction here is audited, so this sits on the critical path of every
// mint, transfer and redeem: if it errors, the initiating flow blocks.
type AuditView struct{}

func (v *AuditView) Call(context view.Context) (interface{}, error) {
	tx, err := ttx.ReceiveTransaction(context)
	if err != nil {
		return nil, errors.Wrap(err, "failed receiving transaction to audit")
	}

	w := ttx.MyAuditorWallet(context)
	if w == nil {
		return nil, errors.New("auditor wallet not found")
	}

	auditor, err := ttx.NewAuditor(context, w)
	if err != nil {
		return nil, errors.Wrap(err, "failed getting auditor service")
	}

	if err := auditor.Validate(tx); err != nil {
		return nil, errors.Wrapf(err, "failed validating tx [%s]", tx.ID())
	}

	logger.Infof("auditing tx [%s]: validated, approving", tx.ID())

	// Approving also records the tx in the auditor's own database, which is
	// what backs its balance and history endpoints.
	res, err := context.RunView(ttx.NewAuditApproveView(w, tx))
	if err != nil {
		return nil, errors.Wrapf(err, "failed approving tx [%s]", tx.ID())
	}

	return res, nil
}
