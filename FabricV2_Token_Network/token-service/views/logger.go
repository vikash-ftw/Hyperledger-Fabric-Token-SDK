// Package views holds the Token SDK business logic shared by all three nodes.
//
// It is a shared module because the Smart Client derives a view's on-wire
// identifier from its Go package path + type name, so initiator and responder
// must reference the same type. Per-node copies would never match.
package views

import "github.com/hyperledger-labs/fabric-smart-client/platform/common/services/logging"

var logger = logging.MustGetLogger()
