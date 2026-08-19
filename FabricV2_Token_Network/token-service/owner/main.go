// Command owner runs the Token Service owner node: it holds the end-user
// wallets, responds to incoming issues and transfers, and exposes
// transfer/redeem/balance/history over REST.
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

var logger = views.NodeLogger("owner")

func main() {
	confDir := os.Getenv("CONF_DIR")
	if confDir == "" {
		confDir = "./conf"
	}
	restAddr := os.Getenv("REST_ADDR")
	if restAddr == "" {
		restAddr = ":9200"
	}

	// Before any node startup: the container healthcheck runs this same binary,
	// and only needs to probe the instance that is already serving.
	if views.IsHealthCheck() {
		views.HealthCheckAndExit(restAddr)
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

	// Responders must be registered AFTER Start, and are keyed by the
	// INITIATOR's view type. An incoming mint and an incoming transfer look the
	// same from here, so one responder covers both.
	registry := viewregistry.GetRegistry(fsc)

	if err := registry.RegisterResponder(&views.AcceptCashView{}, &views.IssueCashView{}); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed registering AcceptCashView for IssueCashView"))
		os.Exit(1)
	}
	if err := registry.RegisterResponder(&views.AcceptCashView{}, &views.TransferView{}); err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed registering AcceptCashView for TransferView"))
		os.Exit(1)
	}

	logger.Infof("token-service owner started, conf dir: %s", confDir)

	svc := TokenService{FSC: fsc}

	// Not fatal: without the order store the node still serves transfers,
	// balances, history and wallet registration.
	orders, err := newOrderStore(fsc)
	if err != nil {
		logger.Errorf("%+v", errors.Wrap(err, "failed opening order store; /lock, /confirm and /cancel will be unavailable"))
	} else {
		svc.orders = orders
		defer func() {
			if err := orders.Close(); err != nil {
				logger.Errorf("failed closing order store: %v", err)
			}
		}()
	}

	srv := startREST(svc, restAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.Infof("shutting down owner...")
	shutdownREST(srv)
}
