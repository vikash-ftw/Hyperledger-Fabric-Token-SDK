// Command auditor runs the Token Service auditor node: it countersigns
// and records every transaction on the network, and exposes independent
// balance/history views over REST.
//
// It is also the P2P bootstrap node for the other two, so it must be running
// and healthy before issuer and owner start.
package main

import (
	"os"
	"os/signal"
	"syscall"

	fscnode "github.com/hyperledger-labs/fabric-smart-client/node"
	viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view/services/view"
	"github.com/hyperledger-labs/fabric-token-sdk/token/services/ttx"
	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
	tokensdk "github.com/vikash-ftw/assetchain/token-service/views/sdk"
)

var logger = views.NodeLogger("auditor")

func main() {
	confDir := os.Getenv("CONF_DIR")
	if confDir == "" {
		confDir = "./conf"
	}
	restAddr := os.Getenv("REST_ADDR")
	if restAddr == "" {
		restAddr = ":9000"
	}

	fsc := fscnode.NewWithConfPath(confDir)

	if err := fsc.InstallSDK(tokensdk.NewSDK(fsc)); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed installing token sdk"))
		os.Exit(1)
	}

	if err := fsc.Start(); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed starting fsc node"))
		os.Exit(1)
	}
	defer fsc.Stop()

	// Triggered automatically during endorsement collection for every
	// transaction on this network. Unregistered, every mint and transfer hangs
	// waiting for auditor approval.
	registry := viewregistry.GetRegistry(fsc)
	if err := registry.RegisterResponder(&views.AuditView{}, &ttx.AuditingViewInitiator{}); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed registering AuditView"))
		os.Exit(1)
	}

	logger.Infof("token-service auditor started, conf dir: %s", confDir)

	svc := TokenService{FSC: fsc}
	srv := startREST(svc, restAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Infof("shutting down auditor...")
	shutdownREST(srv)
}
