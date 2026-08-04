package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	token1 "github.com/hyperledger-labs/fabric-token-sdk/token"
	x509crypto "github.com/hyperledger-labs/fabric-token-sdk/token/services/identity/x509/crypto"
	"github.com/pkg/errors"
)

// A walletId becomes a scratch directory name and a CA enrollment ID, so
// anything outside this set is rejected rather than sanitised: silently
// rewriting an identifier the caller believes is unique is worse than refusing
// it. Rejects "..", "/", absolute paths and uppercase by construction.
//
// Accepts a canonical lowercase UUID v4, optionally with a short alphanumeric
// prefix (e.g. "usr-<uuid>").
var walletIDPattern = regexp.MustCompile(
	`^([a-z0-9]{1,16}-)?[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// caConfig is the narrow registrar credential the owner uses to enroll new
// wallets - deliberately NOT the CA admin, see scripts/registerWalletRegistrar.sh.
type caConfig struct {
	URL           string
	Name          string
	TLSCert       string
	RegistrarHome string // FABRIC_CA_CLIENT_HOME for the registrar identity
}

func caConfigFromEnv() (caConfig, error) {
	c := caConfig{
		URL:           os.Getenv("CA_URL"),
		Name:          os.Getenv("CA_NAME"),
		TLSCert:       os.Getenv("CA_TLS_CERT"),
		RegistrarHome: os.Getenv("CA_REGISTRAR_HOME"),
	}
	var missing []string
	if c.URL == "" {
		missing = append(missing, "CA_URL")
	}
	if c.Name == "" {
		missing = append(missing, "CA_NAME")
	}
	if c.TLSCert == "" {
		missing = append(missing, "CA_TLS_CERT")
	}
	if c.RegistrarHome == "" {
		missing = append(missing, "CA_REGISTRAR_HOME")
	}
	if len(missing) > 0 {
		return c, errors.Errorf("wallet registration is not configured: missing %s", strings.Join(missing, ", "))
	}

	return c, nil
}

func ValidateWalletID(walletID string) error {
	if walletID == "" {
		return errors.New("walletId is required")
	}
	if !walletIDPattern.MatchString(walletID) {
		return errors.New("walletId must be a lowercase UUID v4, optionally with a short alphanumeric prefix (e.g. usr-<uuid>)")
	}

	return nil
}

// walletExists guards against reuse. Registration is one-way: v0.10.1 exposes
// no unregister or delete API, so a handle is permanently burned once created
// and re-registering would duplicate or silently shadow the existing identity.
func (s TokenService) walletExists(ctx context.Context, walletID string) (bool, error) {
	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return false, errors.Wrap(err, "failed getting token management service")
	}

	ids, err := tms.WalletManager().OwnerWalletIDs(ctx)
	if err != nil {
		return false, errors.Wrap(err, "failed listing owner wallet ids")
	}
	for _, id := range ids {
		if id == walletID {
			return true, nil
		}
	}

	return false, nil
}

// RegisterWallet enrolls a new owner wallet with the CA and registers it with
// the Token SDK, making it immediately usable by /transfer, /accounts, etc.
//
// Custodial: this node holds the signing key. Material is enrolled into a
// scratch directory, read into memory, handed to the SDK via
// IdentityConfiguration.Raw, and the scratch directory wiped. Nothing is
// written under token-keys/ (mounted read-only) and no key bytes are logged.
func (s TokenService) RegisterWallet(ctx context.Context, walletID string) (string, error) {
	if err := ValidateWalletID(walletID); err != nil {
		return "", err
	}

	ca, err := caConfigFromEnv()
	if err != nil {
		return "", err
	}

	exists, err := s.walletExists(ctx, walletID)
	if err != nil {
		return "", err
	}
	if exists {
		return "", errors.Errorf("wallet [%s] is already registered; wallet handles are never reused", walletID)
	}

	// Removed on every exit path, including errors, so key material never
	// outlives the request.
	scratch, err := os.MkdirTemp("", "enroll-"+walletID+"-")
	if err != nil {
		return "", errors.Wrap(err, "failed creating scratch dir")
	}
	defer func() {
		if rmErr := os.RemoveAll(scratch); rmErr != nil {
			logger.Errorf("failed wiping scratch dir for wallet [%s]: %v", walletID, rmErr)
		}
	}()

	mspDir := filepath.Join(scratch, "msp")

	// Generated, used for the register+enroll pair, discarded with the scratch
	// dir - it never leaves this process.
	secret, err := randomSecret()
	if err != nil {
		return "", errors.Wrap(err, "failed generating enrollment secret")
	}

	if err := s.caRegister(ctx, ca, walletID, secret); err != nil {
		return "", err
	}
	if err := s.caEnroll(ctx, ca, walletID, secret, mspDir); err != nil {
		return "", err
	}
	if err := normalizeKeystore(mspDir); err != nil {
		return "", err
	}

	// With Raw populated the SDK's key manager uses it directly and never
	// touches the URL path (x509/km.go: `if conf == nil { LoadConfig(...) }`),
	// which is what lets token-keys stay read-only.
	conf, err := x509crypto.LoadConfig(mspDir, "")
	if err != nil {
		return "", errors.Wrapf(err, "failed loading enrolled identity for [%s]", walletID)
	}
	raw, err := x509crypto.MarshalConfig(conf)
	if err != nil {
		return "", errors.Wrapf(err, "failed marshalling identity for [%s]", walletID)
	}

	tms, err := token1.GetManagementService(s.FSC)
	if err != nil {
		return "", errors.Wrap(err, "failed getting token management service")
	}
	if err := tms.WalletManager().RegisterOwnerIdentityConfiguration(ctx, token1.IdentityConfiguration{
		ID:  walletID,
		Raw: raw,
	}); err != nil {
		return "", errors.Wrapf(err, "failed registering owner wallet [%s]", walletID)
	}

	// Length only - never the material itself.
	logger.Infof("registered owner wallet [%s] (identity config %d bytes)", walletID, len(raw))

	return walletID, nil
}

func (s TokenService) caRegister(ctx context.Context, ca caConfig, walletID, secret string) error {
	// maxenrollments 1: the identity is enrolled exactly once, here, so a
	// leaked secret cannot be replayed for a second certificate.
	cmd := exec.CommandContext(ctx, "fabric-ca-client", "register",
		"--caname", ca.Name,
		"-u", ca.URL,
		"--id.name", walletID,
		"--id.secret", secret,
		"--id.type", "client",
		"--id.affiliation", "org1",
		"--id.maxenrollments", "1",
		"--tls.certfiles", ca.TLSCert,
	)
	cmd.Env = append(os.Environ(), "FABRIC_CA_CLIENT_HOME="+ca.RegistrarHome)

	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(err, "failed registering [%s] with the CA: %s", walletID, redactSecret(string(out), secret))
	}

	return nil
}

func (s TokenService) caEnroll(ctx context.Context, ca caConfig, walletID, secret, mspDir string) error {
	// The secret is embedded in the enrollment URL, so output is redacted
	// before it can reach a log.
	enrollURL, err := enrollmentURL(ca.URL, walletID, secret)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "fabric-ca-client", "enroll",
		"-u", enrollURL,
		"--caname", ca.Name,
		"-M", mspDir,
		"--tls.certfiles", ca.TLSCert,
	)
	cmd.Env = append(os.Environ(), "FABRIC_CA_CLIENT_HOME="+filepath.Dir(mspDir))

	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(err, "failed enrolling [%s]: %s", walletID, redactSecret(string(out), secret))
	}

	return nil
}

// normalizeKeystore renames the CA-generated key file to priv_sk, which is the
// exact name x509crypto.LoadConfig looks for. In-process equivalent of
// scripts/normalizeKeystores.sh.
func normalizeKeystore(mspDir string) error {
	keystore := filepath.Join(mspDir, "keystore")
	if _, err := os.Stat(filepath.Join(keystore, "priv_sk")); err == nil {
		return nil
	}

	entries, err := os.ReadDir(keystore)
	if err != nil {
		return errors.Wrapf(err, "failed reading keystore")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		return os.Rename(filepath.Join(keystore, e.Name()), filepath.Join(keystore, "priv_sk"))
	}

	return errors.New("no private key found in enrolled keystore")
}
