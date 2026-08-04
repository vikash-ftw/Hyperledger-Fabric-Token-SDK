#!/bin/bash
# Register + enroll the `walletregistrar` CA identity.
#
# The owner node's POST /wallets needs CA credentials at runtime. The obvious
# credential - the CA admin from registerEnroll.sh - is a full superuser
# (Registrar.Roles=*, Revoker, AffiliationMgr, GenCRL, IntermediateCA), so
# mounting it into a network-facing container would let anyone compromising
# that node mint admin/peer/orderer identities, revoke certs and issue an
# intermediate CA.
#
# Wallet enrollment only ever does `register --id.type client` + `enroll`, with
# no --id.attrs and no affiliation management, so this creates the narrowest
# identity that still does that one job.
#
# Fabric CA has no hf.Registrar.MaxEnrollments attribute; enrollment counts are
# bounded per-identity via --id.maxenrollments instead.

echo
echo " ____    _____      _      ____    _____ "
echo "/ ___|  |_   _|    / \    |  _ \  |_   _|"
echo "\___ \    | |     / _ \   | |_) |   | |  "
echo " ___) |   | |    / ___ \  |  _ <    | |  "
echo "|____/    |_|   /_/   \_\ |_| \_\   |_|  "
echo
echo "====== Registering the wallet-registrar CA identity ======"
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

REGISTRAR_NAME="walletregistrar"
REGISTRAR_SECRET="${WALLETREGISTRAR_SECRET:-walletregistrarpw}"
REGISTRAR_MSP_DIR="${PWD}/token-keys/walletregistrar/msp"
# Bounded so a leaked secret cannot mint unlimited registrar certificates.
REGISTRAR_MAX_ENROLLMENTS=5
CA_TLS_CERT="${PWD}/organizations/fabric-ca/org1/tls-cert.pem"

## Preconditions

if ! command -v fabric-ca-client >/dev/null 2>&1; then
  echo "ERROR: fabric-ca-client not found on PATH."
  exit 1
fi

if ! (exec 3<>/dev/tcp/localhost/7054) 2>/dev/null; then
  echo "ERROR: Cannot reach Fabric CA (ca-org1) at localhost:7054."
  echo "Ensure the network and CA containers are up before running this script."
  exit 1
fi
exec 3<&- 3>&-

if [ ! -s "${CA_TLS_CERT}" ]; then
  echo "ERROR: ${CA_TLS_CERT} not found."
  echo "Run scripts/start_fabric-ca.sh and scripts/registerEnroll.sh first."
  exit 1
fi

## Used ONCE here to create the registrar. The admin credential is never
## mounted into any container; only the weaker registrar MSP below is.
export FABRIC_CA_CLIENT_HOME=${PWD}/organizations/peerOrganizations/org1.example.com/

## Register

displayMsg "Registering ${REGISTRAR_NAME}"
fabric-ca-client identity list --caname ca-org1 --id "${REGISTRAR_NAME}" \
  --tls.certfiles "${CA_TLS_CERT}" >/dev/null 2>&1
exists=$?

if [ ${exists} -eq 0 ]; then
  echo "SUCCESS: Identity ${REGISTRAR_NAME} is already registered, skipping registration"
  echo
else
  # Roles=client: may register only client identities, never admin/peer/
  # orderer. DelegateRoles empty: cannot pass registrar powers on, so it cannot
  # bootstrap a stronger identity. Everything else denied outright. Affiliation
  # org1 scopes it to this org's subtree.
  set -x
  fabric-ca-client register --caname ca-org1 \
    --id.name "${REGISTRAR_NAME}" \
    --id.secret "${REGISTRAR_SECRET}" \
    --id.type client \
    --id.affiliation org1 \
    --id.maxenrollments ${REGISTRAR_MAX_ENROLLMENTS} \
    --id.attrs 'hf.Registrar.Roles=client,hf.Registrar.DelegateRoles=,hf.Registrar.Attributes=,hf.Revoker=false,hf.AffiliationMgr=false,hf.GenCRL=false,hf.IntermediateCA=false' \
    --tls.certfiles "${CA_TLS_CERT}"
  res=$?
  set +x
  verifyResult $res "Registered ${REGISTRAR_NAME}" "Failed to register ${REGISTRAR_NAME}!"
fi

## Enroll

displayMsg "Generating the ${REGISTRAR_NAME} msp"
mkdir -p "${REGISTRAR_MSP_DIR}"
set -x
fabric-ca-client enroll -u https://${REGISTRAR_NAME}:${REGISTRAR_SECRET}@localhost:7054 \
  --caname ca-org1 -M "${REGISTRAR_MSP_DIR}" --tls.certfiles "${CA_TLS_CERT}"
res=$?
set +x
verifyResult $res "Generated ${REGISTRAR_NAME} msp" "Failed to generate ${REGISTRAR_NAME} msp!"

cp ${PWD}/organizations/peerOrganizations/org1.example.com/msp/config.yaml "${REGISTRAR_MSP_DIR}/config.yaml"

## Normalise to priv_sk - see scripts/normalizeKeystores.sh for why.
KEYSTORE_DIR="${REGISTRAR_MSP_DIR}/keystore"
if [ ! -f "${KEYSTORE_DIR}/priv_sk" ]; then
  sk=$(ls -1 "${KEYSTORE_DIR}" 2>/dev/null | head -1)
  if [ -n "${sk}" ]; then
    mv "${KEYSTORE_DIR}/${sk}" "${KEYSTORE_DIR}/priv_sk"
    echo "SUCCESS: normalised keystore to priv_sk"
  fi
fi

displayMsg "Resulting identity (verify the privileges are as intended)"
fabric-ca-client identity list --caname ca-org1 --id "${REGISTRAR_NAME}" \
  --tls.certfiles "${CA_TLS_CERT}"

echo
echo "========= Registered the wallet-registrar CA identity =========== "
echo
echo "MSP written to: ${REGISTRAR_MSP_DIR}"
echo "This MSP - and NOT the CA admin - is what gets mounted into the owner node."
echo

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
