// Package sdk assembles the Token SDK stack that all three nodes install.
//
// As of Token SDK v0.10.x, tokensdk.NewSDK() wires the platform but registers
// no drivers; the application must provide them into named dig groups or the
// node starts and then fails at runtime ("no network driver found"). Under
// v0.3.0 importing token/sdk did this implicitly, so no such file existed.
//
// Modelled on integration/token/common/sdk/fall/sdk.go, trimmed to fabric +
// fabtoken. The token driver set must agree with the public parameters and the
// chaincode image: omitting fabtoken makes every token operation fail.
package sdk

import (
	"errors"

	dig2 "github.com/hyperledger-labs/fabric-smart-client/platform/common/sdk/dig"
	"github.com/hyperledger-labs/fabric-smart-client/platform/view/services"
	fabtoken "github.com/hyperledger-labs/fabric-token-sdk/token/core/fabtoken/v1/driver"
	tsdk "github.com/hyperledger-labs/fabric-token-sdk/token/sdk"
	tokensdk "github.com/hyperledger-labs/fabric-token-sdk/token/sdk/dig"
	tnetfabric "github.com/hyperledger-labs/fabric-token-sdk/token/services/network/fabric"
	"go.uber.org/dig"
)

// SDK is the Token SDK stack for this network.
//
// tokensdk.NewSDK already stacks token -> fabric -> view, so nodes install
// this and nothing else; installing the fabric or view SDK alongside it would
// register the same services twice.
type SDK struct {
	dig2.SDK
}

func NewSDK(registry services.Registry) *SDK {
	return &SDK{SDK: tokensdk.NewSDK(registry)}
}

func (p *SDK) Install() error {
	// RegisterTokenDriverDependencies provides the shared dependencies every
	// token driver constructor asks for; token/sdk/driver.go documents it as
	// mandatory for custom SDKs.
	if err := errors.Join(
		tsdk.RegisterTokenDriverDependencies(p.Container()),
		p.Container().Provide(tnetfabric.NewGenericDriver, dig.Group("network-drivers")),
		p.Container().Provide(fabtoken.NewDriver, dig.Group("token-drivers")),
	); err != nil {
		return err
	}

	return p.SDK.Install()
}
