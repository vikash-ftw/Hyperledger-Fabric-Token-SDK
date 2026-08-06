#!/bin/bash


echo
echo " _____   ___    _  __  _____   _   _ "
echo "|_   _| / _ \  | |/ / | ____| | \ | |"
echo "  | |  | | | | | ' /  |  _|   |  \| |"
echo "  | |  | |_| | | . \  | |___  | |\  |"
echo "  |_|   \___/  |_|\_\ |_____| |_| \_|"
echo
echo "====== Generating Token SDK Public Parameters ======"
echo
echo

export PATH=${PWD}/../bin:$PATH
export FABRIC_CFG_PATH=$PWD/../config/

displayMsg() {
    msg=$1
    echo "====== ${msg} ====="
}

verifyResult() {
  if [ $1 -eq 0 ]; then
    echo "SUCCESS: $2"
  else
    echo "ERROR: $3"
    exit 1
  fi
  echo
}

## Parse args

FORCE=${FORCE:-false}
for arg in "$@"; do
  case ${arg} in
    --force)
      FORCE=true
      ;;
  esac
done

OUTPUT_DIR=${PWD}/token-cc
# Must match docker/Dockerfile.token-cc and token-service/*/go.mod.
TOKEN_SDK_VERSION=v0.10.1

## Preconditions

if ! command -v tokengen >/dev/null 2>&1; then
  echo "ERROR: tokengen not found on PATH."
  echo "Install it with:"
  echo "  go install github.com/hyperledger-labs/fabric-token-sdk/cmd/tokengen@${TOKEN_SDK_VERSION}"
  exit 1
fi

# A v0.3.0 binary exposes `gen fabtoken`; v0.10.x renamed it to `gen fabtoken.v1`
# after the driver identifier. Without this check the run fails further down with
# "unknown flag: --issuers" - misleading, because the flag is fine and it is the
# subcommand that does not exist, so tokengen parses it as a positional arg.
if ! tokengen gen --help 2>&1 | grep -q 'fabtoken\.v1'; then
  echo "ERROR: tokengen is too old - it has no 'gen fabtoken.v1' subcommand."
  echo "  binary:  $(command -v tokengen)"
  if command -v go >/dev/null 2>&1; then
    found=$(go version -m "$(command -v tokengen)" 2>/dev/null | awk '$1=="mod"{print $3; exit}')
    echo "  version: ${found:-unknown} (need ${TOKEN_SDK_VERSION})"
  fi
  echo
  echo "Reinstall the matching version:"
  echo "  go install github.com/hyperledger-labs/fabric-token-sdk/cmd/tokengen@${TOKEN_SDK_VERSION}"
  echo
  echo "It must match the version in docker/Dockerfile.token-cc and token-service/*/go.mod,"
  echo "or the chaincode cannot parse the parameters this script produces."
  exit 1
fi

if [ ! -d "${PWD}/token-keys/issuer/msp/signcerts" ] || [ -z "$(ls -A "${PWD}/token-keys/issuer/msp/signcerts" 2>/dev/null)" ]; then
  echo "ERROR: ${PWD}/token-keys/issuer/msp/signcerts not found or empty."
  echo "Run scripts/registerTokenIdentities.sh first to enroll the fscissuer identity."
  exit 1
fi

if [ ! -d "${PWD}/token-keys/auditor/msp/signcerts" ] || [ -z "$(ls -A "${PWD}/token-keys/auditor/msp/signcerts" 2>/dev/null)" ]; then
  echo "ERROR: ${PWD}/token-keys/auditor/msp/signcerts not found or empty."
  echo "Run scripts/registerTokenIdentities.sh first to enroll the fscauditor identity."
  exit 1
fi

## Public parameters are immutable once the chaincode is committed.
## Regenerating afterwards invalidates every token already issued, since
## validators would enforce rules tied to a different issuer/auditor set.
if [ -d "${OUTPUT_DIR}" ] && [ -n "$(ls -A "${OUTPUT_DIR}" 2>/dev/null)" ]; then
  if [ "${FORCE}" != "true" ]; then
    echo "ERROR: ${OUTPUT_DIR} already exists and is not empty."
    echo
    echo "WARNING: Public parameters are immutable once the token chaincode is"
    echo "committed to a channel. Regenerating them will produce a new set of"
    echo "public parameters and INVALIDATE any tokens already issued under the"
    echo "existing ones. Do not do this against a live network unless you know"
    echo "exactly what you are doing."
    echo
    echo "Re-run with --force (or FORCE=true) if you deliberately want to"
    echo "overwrite ${OUTPUT_DIR}."
    exit 1
  fi
  displayMsg "FORCE set - proceeding to overwrite existing ${OUTPUT_DIR}"
fi

mkdir -p "${OUTPUT_DIR}"

## Generate public parameters

## The subcommand is the versioned driver identifier: "fabtoken.v1", not the
## bare "fabtoken" of v0.3.0. tokengen MUST be built from the same tag as the
## chaincode image - a skew produces parameters the chaincode cannot parse.
##
## The output name follows the driver version: fabtoken1_pp.json.
displayMsg "Running tokengen"
set -x
tokengen gen fabtoken.v1 \
  --issuers ${PWD}/token-keys/issuer/msp \
  --auditors ${PWD}/token-keys/auditor/msp \
  --output ${PWD}/token-cc
res=$?
set +x
verifyResult $res "Generated public parameters" "Failed to generate public parameters!"

displayMsg "Generated files"
ls -lR "${OUTPUT_DIR}"

echo
echo "========= Generated Token SDK Public Parameters =========== "

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
