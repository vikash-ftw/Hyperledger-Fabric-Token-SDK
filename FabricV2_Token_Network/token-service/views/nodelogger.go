package views

import "github.com/hyperledger-labs/fabric-smart-client/platform/common/services/logging"

// This package's import path with "/" replaced by "." - the shape
// logging.loggerName() builds before applying replacers.
const pkgLoggerPrefix = "github.com.vikash-ftw.assetchain.token-service.views"

func init() {
	// Collapses the derived name to "token-service.<role>", which is what
	// logging.spec in core.yaml is written against.
	logging.RegisterReplacer(pkgLoggerPrefix, "token-service")
}

// NodeLogger returns a logger named "token-service.<role>".
//
// Node main packages must use this rather than logging.MustGetLogger: that
// function derives a name by slicing the caller's package path at its last
// "/", and package `main` has none, so it panics on a -1 index before main()
// runs. Passing an explicit name does not help - the introspection is
// unconditional. Calling from here works because this path contains "/".
func NodeLogger(role string) logging.Logger {
	return logging.MustGetLogger(role)
}
