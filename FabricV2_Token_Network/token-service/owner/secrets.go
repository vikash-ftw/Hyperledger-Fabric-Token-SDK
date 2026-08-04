package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// randomSecret returns a single-use CA enrollment secret. It is never
// persisted or returned to the caller, and with --id.maxenrollments 1 on the
// registered identity it cannot be replayed even if it leaked.
func randomSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "failed reading random bytes")
	}

	return hex.EncodeToString(b), nil
}

// enrollmentURL builds https://<id>:<secret>@host:port via net/url so the
// credentials are escaped rather than concatenated.
func enrollmentURL(caURL, id, secret string) (string, error) {
	u, err := url.Parse(caURL)
	if err != nil {
		return "", errors.Wrapf(err, "invalid CA_URL")
	}
	u.User = url.UserPassword(id, secret)

	return u.String(), nil
}

// redactSecret is the last line of defence against a credential reaching a log:
// fabric-ca-client echoes the enrollment URL, secret included, and its output
// is wrapped into errors that get logged and returned to the HTTP caller.
func redactSecret(s, secret string) string {
	if secret == "" {
		return s
	}

	return strings.ReplaceAll(s, secret, "***")
}
