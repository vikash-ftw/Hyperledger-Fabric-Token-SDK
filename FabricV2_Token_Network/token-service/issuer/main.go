// Command issuer runs the Token Service issuer node: a Fabric Smart
// Client node that mints tokens, fronted by a small REST API.
package main

import (
	"os"
	"os/signal"
	"syscall"

	fscnode "github.com/hyperledger-labs/fabric-smart-client/node"
	viewregistry "github.com/hyperledger-labs/fabric-smart-client/platform/view/services/view"
	"github.com/pkg/errors"

	"github.com/vikash-ftw/assetchain/token-service/views"
	tokensdk "github.com/vikash-ftw/assetchain/token-service/views/sdk"
)

var logger = views.NodeLogger("issuer")

func main() {
	// A DIRECTORY containing core.yaml, not the file itself.
	confDir := os.Getenv("CONF_DIR")
	if confDir == "" {
		confDir = "./conf"
	}
	restAddr := os.Getenv("REST_ADDR")
	if restAddr == "" {
		restAddr = ":9100"
	}

	fsc := fscnode.NewWithConfPath(confDir)

	// One SDK only: this already stacks the Fabric and View platforms, and
	// installing those separately would register the same services twice.
	if err := fsc.InstallSDK(tokensdk.NewSDK(fsc)); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed installing token sdk"))
		os.Exit(1)
	}

	if err := fsc.Start(); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed starting fsc node"))
		os.Exit(1)
	}
	defer fsc.Stop()

	// The issuer also has to RESPOND to redeems: burning returns tokens to
	// it, so it must endorse the burn. Without this the owner's redeem fails
	// with "endpoint not found for identity <empty>" while mint and transfer
	// keep working.
	registry := viewregistry.GetRegistry(fsc)
	if err := registry.RegisterResponder(&views.IssuerRedeemAcceptView{}, &views.RedeemView{}); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed registering IssuerRedeemAcceptView"))
		os.Exit(1)
	}

	logger.Infof("token-service issuer started, conf dir: %s", confDir)

	svc := TokenService{FSC: fsc}
	srv := startREST(svc, restAddr)

	// Close the REST listener before stopping the node so in-flight requests
	// are not cut off mid-view.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Infof("shutting down issuer...")
	shutdownREST(srv)
}
