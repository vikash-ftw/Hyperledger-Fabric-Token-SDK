#!/bin/bash
# Deploy the Token Chaincode as CCAAS (Chaincode as a Service).
#
# Unlike standard chaincode, a CCAAS package carries only a connection.json
# pointer to a chaincode service we run ourselves; the peer builds nothing and
# just dials that address.
#
# ONE shared package and container, not one per peer: approveformyorg names
# exactly one package-id, and package-id is a hash over the package bytes
# including connection.json. Both peers must therefore install byte-identical
# packages pointing at the same chaincode address.
#
# Ordering that matters: the chaincode container is started BEFORE
# install/approve/commit, because install-time handshakes and the
# --init-required invocation both dial the chaincode address.

# Default values for channelname and version
CHANNEL_NAME="${1:-samplechannel}"
VERSION="${2:-1}"

MAX_RETRY="3"

CHAINCODE_NAME="token-cc"
PACKAGE_LABEL="token-cc_v${VERSION}"
TOKEN_CC_IMAGE="token-cc:latest"

# COMPOSE_PROJECT_NAME + "_fbn", since that network has no `name:` override.
# Must match docker/docker-compose-token-cc.yaml.
DOCKER_NETWORK_NAME="fabric_net_fbn"

echo
echo " ____    _____      _      ____    _____ "
echo "/ ___|  |_   _|    / \    |  _ \  |_   _|"
echo "\___ \    | |     / _ \   | |_) |   | |  "
echo " ___) |   | |    / ___ \  |  _ <    | |  "
echo "|____/    |_|   /_/   \_\ |_| \_\   |_|  "
echo
echo "Deploy Token Chaincode (CCAAS) on Channel - $CHANNEL_NAME"
echo
echo "Chaincode Name - $CHAINCODE_NAME with version - $VERSION"
echo

# Adding fabric bin to path
export PATH=${PWD}/../bin:$PATH
# Adding fabric config
export FABRIC_CFG_PATH=$PWD/../config/

export CORE_PEER_TLS_ENABLED=true
export ORDERER_CA=${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem

export CORE_PEER_LOCALMSPID=""
export CORE_PEER_TLS_ROOTCERT_FILE=""
export CORE_PEER_MSPCONFIGPATH=""
export CORE_PEER_ADDRESS=""

ORG1_PEERS_PORTS=(7051 8051)

# Org1 Peers
PEER0_ORG1_CA=${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt
PEER1_ORG1_CA=${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer1.org1.example.com/tls/ca.crt
ORG1_PEERS_CAS=($PEER0_ORG1_CA $PEER1_ORG1_CA)

PACKAGE_FILE=${PWD}/token-cc/token-cc.tar.gz
# The network's existing top-level .env, NOT a token-cc-specific file;
# writeTokenCCEnvFile() only upserts the single CHAINCODE_ID line.
ENV_FILE=${PWD}/.env
COMPOSE_FILE_TOKEN_CC=${PWD}/docker/docker-compose-token-cc.yaml

PACKAGE_ID=""

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

## Preconditions

checkPreconditions() {
  displayMsg "Checking preconditions"

  if ! command -v docker >/dev/null 2>&1; then
    echo "ERROR: docker not found on PATH."
    exit 1
  fi

  if ! docker info >/dev/null 2>&1; then
    echo "ERROR: Docker daemon is not reachable."
    echo "Start Docker before running this script."
    exit 1
  fi

  if ! docker image inspect ${TOKEN_CC_IMAGE} >/dev/null 2>&1; then
    echo "ERROR: Docker image ${TOKEN_CC_IMAGE} not found."
    echo "Run scripts/buildTokenCC.sh first."
    exit 1
  fi

  if [ ! -s "${PWD}/token-cc/fabtoken1_pp.json" ]; then
    echo "ERROR: ${PWD}/token-cc/fabtoken1_pp.json not found or empty."
    echo "Run scripts/generateTokenParams.sh first."
    exit 1
  fi

  if ! docker network inspect ${DOCKER_NETWORK_NAME} >/dev/null 2>&1; then
    echo "ERROR: Docker network ${DOCKER_NETWORK_NAME} not found."
    echo "Start the base network (scripts/start_network.sh) first."
    exit 1
  fi

  if [ ! -f "${ENV_FILE}" ]; then
    echo "ERROR: ${ENV_FILE} not found."
    echo "This script upserts CHAINCODE_ID into the network's existing .env - it does not create one from scratch."
    exit 1
  fi

  for i in "${!ORG1_PEERS_PORTS[@]}"; do
    local port=${ORG1_PEERS_PORTS[$i]}
    if ! (exec 3<>/dev/tcp/localhost/${port}) 2>/dev/null; then
      echo "ERROR: peer${i}.org1.example.com is not reachable on localhost:${port}."
      echo "Ensure the network is up before running this script."
      exit 1
    fi
    exec 3<&- 3>&-
  done

  echo "SUCCESS: preconditions met"
  echo
}

setGlobalVarsForOrg1() {
  export CORE_PEER_LOCALMSPID="Org1MSP"
  export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp
  # using peer0 of Org1
  export CORE_PEER_TLS_ROOTCERT_FILE=$PEER0_ORG1_CA
  export CORE_PEER_ADDRESS=localhost:${ORG1_PEERS_PORTS[0]}
}

setGlobalVarsForPeer() {
  local i=$1
  export CORE_PEER_LOCALMSPID="Org1MSP"
  export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp
  export CORE_PEER_TLS_ROOTCERT_FILE=${ORG1_PEERS_CAS[$i]}
  export CORE_PEER_ADDRESS=localhost:${ORG1_PEERS_PORTS[$i]}
}

# `--lang ccaas` does not exist on peer 2.5.9 ("unknown chaincodeType: CCAAS"),
# so the package is built by hand: metadata.json + code.tar.gz(connection.json),
# tar'd together. Mirrors fabric-samples' deployCCAAS.sh.
packageChaincode() {
  displayMsg "--- Packaging Token Chaincode (CCAAS) ---"

  local tempdir
  tempdir=$(mktemp -d)
  mkdir -p "${tempdir}/src" "${tempdir}/pkg"

  cat >"${tempdir}/src/connection.json" <<CONN_EOF
{
  "address": "token-cc:9999",
  "dial_timeout": "10s",
  "tls_required": false
}
CONN_EOF

  cat >"${tempdir}/pkg/metadata.json" <<METADATA_EOF
{
    "type": "ccaas",
    "label": "${PACKAGE_LABEL}"
}
METADATA_EOF

  tar -C "${tempdir}/src" -czf "${tempdir}/pkg/code.tar.gz" .
  tar -C "${tempdir}/pkg" -czf "${PACKAGE_FILE}" metadata.json code.tar.gz
  rm -rf "${tempdir}"

  if [ ! -s "${PACKAGE_FILE}" ]; then
    echo "ERROR: Failed to build ${PACKAGE_FILE}"
    exit 1
  fi
  echo "SUCCESS: Token Chaincode packaged at ${PACKAGE_FILE}"
  echo

  displayMsg "--- Calculating Token Chaincode PACKAGE_ID ---"
  set -x
  PACKAGE_ID=$(peer lifecycle chaincode calculatepackageid ${PACKAGE_FILE})
  res=$?
  set +x
  echo "PACKAGE_ID: ${PACKAGE_ID}" >./logs/calculatepackageid_token_cc_log.txt
  cat ./logs/calculatepackageid_token_cc_log.txt
  verifyResult $res "Calculated Token Chaincode PACKAGE_ID" "Failed to calculate Token Chaincode PACKAGE_ID!"
}

installChaincodeOnPeer() {
  local i=$1
  displayMsg "--- Installing Token Chaincode on peer${i} ---"
  setGlobalVarsForPeer ${i}
  local rc=1
  local COUNTER=0
  while [ $rc -ne 0 -a $COUNTER -lt $MAX_RETRY ] ; do
    sleep 1
    if [ $COUNTER -gt 0 ]; then
      echo "Command Failed - Retrying ..."
      echo "-- Retry Attempt $COUNTER --"
      sleep 2
    fi
    set -x
    peer lifecycle chaincode install ${PACKAGE_FILE} >&./logs/install_token_cc_peer${i}_log.txt
    res=$?
    set +x
    rc=$res
    COUNTER=$((COUNTER + 1))
    cat ./logs/install_token_cc_peer${i}_log.txt
  done
  verifyResult $res "Token Chaincode installed on peer${i}" "Failed to install Token Chaincode on peer${i}!"
  sleep 2
}

# Returns the reported PACKAGE_ID on stdout; diagnostics go to stderr so
# command-substitution callers capture only the id.
queryInstalledOnPeer() {
  local i=$1
  setGlobalVarsForPeer ${i}
  set -x
  peer lifecycle chaincode queryinstalled >&./logs/query_install_token_cc_peer${i}_log.txt
  res=$?
  set +x
  cat ./logs/query_install_token_cc_peer${i}_log.txt >&2
  verifyResult $res "Token Chaincode query executed successfully for peer${i}" "Token Chaincode query failed for peer${i}!" >&2
  sed -n "/${PACKAGE_LABEL}/{s/^Package ID: //; s/, Label:.*$//; p;}" ./logs/query_install_token_cc_peer${i}_log.txt
}

# Must match the PACKAGE_ID calculated before anything was installed, since
# both peers install byte-identical content.
verifyInstalledPackageId() {
  local i=$1
  local reported
  reported=$(queryInstalledOnPeer ${i})
  if [ "${reported}" != "${PACKAGE_ID}" ]; then
    echo "ERROR: peer${i} reports PACKAGE_ID (${reported}) different from the calculated PACKAGE_ID (${PACKAGE_ID})."
    echo "Both peers must install byte-identical packages so they share one approved package-id."
    exit 1
  fi
  echo "SUCCESS: peer${i} reports the expected PACKAGE_ID"
  echo
}

# In-place replace (or append), never an overwrite: .env also carries
# IMAGE_TAG_*/COMPOSE_PROJECT_NAME that start_network.sh depends on.
writeTokenCCEnvFile() {
  displayMsg "--- Writing CHAINCODE_ID to ${ENV_FILE} ---"
  if grep -q "^CHAINCODE_ID=" "${ENV_FILE}"; then
    sed -i "s|^CHAINCODE_ID=.*|CHAINCODE_ID=${PACKAGE_ID}|" "${ENV_FILE}"
  else
    printf '\nCHAINCODE_ID=%s\n' "${PACKAGE_ID}" >>"${ENV_FILE}"
  fi
  grep "^CHAINCODE_ID=" "${ENV_FILE}"
  echo
}

# Before approve/commit/init - see the header for why that ordering matters.
startChaincodeContainer() {
  displayMsg "--- Starting Token Chaincode container ---"
  set -x
  docker-compose --env-file ${ENV_FILE} -f ${COMPOSE_FILE_TOKEN_CC} up -d >&./logs/token_cc_container_up_log.txt
  res=$?
  set +x
  cat ./logs/token_cc_container_up_log.txt
  verifyResult $res "Token Chaincode container started" "Failed to start Token Chaincode container!"
  sleep 3
}

approveForMyOrg() {
  displayMsg "--- Approving Token Chaincode for Org1 ---"
  setGlobalVarsForOrg1
  sleep 2
  set -x
  peer lifecycle chaincode approveformyorg -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls $CORE_PEER_TLS_ENABLED --cafile $ORDERER_CA --channelID $CHANNEL_NAME --name ${CHAINCODE_NAME} --version ${VERSION} --package-id ${PACKAGE_ID} --sequence ${VERSION} --init-required >&./logs/approve_token_cc_log.txt
  res=$?
  set +x
  cat ./logs/approve_token_cc_log.txt
  verifyResult $res "Token Chaincode definition approved on peer0 of Org1 on channel '$CHANNEL_NAME'" "Token Chaincode definition approval failed on peer0 of Org1 on channel '$CHANNEL_NAME'!"
}

checkCommitReadiness() {
  displayMsg "--- Checking commit readiness of Token Chaincode ---"
  setGlobalVarsForOrg1
  sleep 2
  set -x
  peer lifecycle chaincode checkcommitreadiness --channelID $CHANNEL_NAME --name ${CHAINCODE_NAME} --version ${VERSION} --sequence ${VERSION} --init-required --output json >&./logs/commitReadiness_token_cc_log.txt
  res=$?
  set +x
  cat ./logs/commitReadiness_token_cc_log.txt
  verifyResult $res "Token Chaincode commit readiness checked on peer0 of Org1 for channel '$CHANNEL_NAME'" "Token Chaincode commit readiness FAILED! on peer0 of Org1 for channel '$CHANNEL_NAME'"
}

commitChaincodeDefinition() {
  displayMsg "--- Committing the Token Chaincode on Org1 ---"
  setGlobalVarsForOrg1
  local rc=1
  local COUNTER=0
  while [ $rc -ne 0 -a $COUNTER -lt $MAX_RETRY ] ; do
    sleep 1
    if [ $COUNTER -gt 0 ]; then
      echo "Command Failed - Retrying ..."
      echo "-- Retry Attempt $COUNTER --"
      sleep 2
    fi
    set -x
    peer lifecycle chaincode commit -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls $CORE_PEER_TLS_ENABLED --cafile $ORDERER_CA --channelID $CHANNEL_NAME --name ${CHAINCODE_NAME} --version ${VERSION} --sequence ${VERSION} --init-required >&./logs/token_cc_commit_log.txt
    res=$?
    set +x
    rc=$res
    COUNTER=$((COUNTER + 1))
    cat ./logs/token_cc_commit_log.txt
  done
  verifyResult $res "Token Chaincode committed on ${CHANNEL_NAME} for peers of Org1" "Failed!! - Token Chaincode failed to commit on ${CHANNEL_NAME} for peers of Org1!"
}

# Writes the public parameters (baked into the image) onto the ledger.
invokeInitTokenCC() {
  displayMsg "--- Invoking init on Token Chaincode ---"
  setGlobalVarsForOrg1
  sleep 2
  set -x
  peer chaincode invoke -o localhost:7050 --ordererTLSHostnameOverride orderer.example.com --tls $CORE_PEER_TLS_ENABLED --cafile $ORDERER_CA \
    --peerAddresses localhost:${ORG1_PEERS_PORTS[0]} --tlsRootCertFiles ${ORG1_PEERS_CAS[0]} \
    --peerAddresses localhost:${ORG1_PEERS_PORTS[1]} --tlsRootCertFiles ${ORG1_PEERS_CAS[1]} \
    -C $CHANNEL_NAME -n ${CHAINCODE_NAME} --isInit -c '{"Args":["init"]}' >&./logs/invoke_init_token_cc_log.txt
  res=$?
  set +x
  cat ./logs/invoke_init_token_cc_log.txt
  verifyResult $res "Token Chaincode init invoked on channel '$CHANNEL_NAME'" "Failed to invoke init on Token Chaincode on channel '$CHANNEL_NAME'!"
}

queryCommitted() {
  displayMsg "--- Checking the committed Token Chaincode on Channel - '$CHANNEL_NAME' ---"
  setGlobalVarsForOrg1
  sleep 2
  set -x
  peer lifecycle chaincode querycommitted --channelID $CHANNEL_NAME --name ${CHAINCODE_NAME} --output json >&./logs/query_commit_token_cc_log.txt
  res=$?
  set +x
  cat ./logs/query_commit_token_cc_log.txt
  verifyResult $res "Token Chaincode commit query on ${CHANNEL_NAME} executed successfully" "Failed!! - Token Chaincode commit query execution failed on ${CHANNEL_NAME}!"
}

checkPreconditions

## Computes PACKAGE_ID before anything is installed, so it can reach the
## chaincode container's env before that container ever starts.
packageChaincode

writeTokenCCEnvFile
## Must be running before install and the init invocation reach it.
startChaincodeContainer

installChaincodeOnPeer 0
installChaincodeOnPeer 1

displayMsg "--- Verifying installed PACKAGE_ID on both peers ---"
verifyInstalledPackageId 0
verifyInstalledPackageId 1

approveForMyOrg
checkCommitReadiness
commitChaincodeDefinition

invokeInitTokenCC

queryCommitted

echo
echo "========= Token Chaincode successfully deployed (CCAAS) on channel $CHANNEL_NAME  =========== "

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
