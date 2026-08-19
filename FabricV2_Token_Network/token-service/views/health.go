package views

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// HealthCheckAndExit probes this node's own /healthz and exits 0 or 1.
//
// It exists so the container healthcheck can run the node binary itself. The
// alternative is a healthcheck that shells out to curl, which means installing
// curl, which means an apt-get against deb.debian.org at build time - a network
// dependency the runtime image otherwise does not have.
//
// Never returns.
func HealthCheckAndExit(restAddr string) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get("http://localhost" + restAddr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "health check failed:", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "health check returned", resp.Status)
		os.Exit(1)
	}

	os.Exit(0)
}

// IsHealthCheck reports whether this process was started as a healthcheck rather
// than as a node. Checked against os.Args directly rather than through the flag
// package, so it runs before anything else can consume the command line.
func IsHealthCheck() bool {
	return len(os.Args) > 1 && os.Args[1] == "-health"
}
