#!/bin/bash
# build and start the three Token Service (Fabric Smart Client) nodes
#
# The preconditions below exist because each one, unmet, produces an opaque
# failure deep inside a container rather than a clear message here.
#
# NODE STATE IS PRESERVED ACROSS RESTARTS - DO NOT ADD `down -v` HERE.
#
# The v0.3.0 script wiped node volumes on every start to work around an FSC bug
# where the delivery client skipped the channel CONFIG block and Broadcast()
# failed forever with "no consensus type set". That is now neither needed nor
# safe: Committer.ReloadConfigTransactions() replays the persisted config tx on
# channel open, and node state now lives in Postgres including the key_store
# holding private keys for every pseudonymous identity this node was issued.
# Those keys are not on the ledger and cannot be re-derived, so wiping the
# volume makes every token previously sent here permanently unspendable.
#
# For a genuine clean slate run with RESET=true, which warns and confirms.

echo
echo " ____    _____      _      ____    _____ "
echo "/ ___|  |_   _|    / \    |  _ \  |_   _|"
echo "\___ \    | |     / _ \   | |_) |   | |  "
echo " ___) |   | |    / ___ \  |  _ <    | |  "
echo "|____/    |_|   /_/   \_\ |_| \_\   |_|  "
echo
echo "====== Starting Token Service Nodes ======"
echo
echo

CHANNEL_NAME="${1:-samplechannel}"
CHAINCODE_NAME="token-cc"
DOCKER_NETWORK_NAME="fabric_net_fbn"
COMPOSE_FILE_TOKEN_NODES=${PWD}/docker/docker-compose-token-nodes.yaml

# Adding fabric bin to path
export PATH=${PWD}/../bin:$PATH
# Adding fabric config
export FABRIC_CFG_PATH=$PWD/../config/

export CORE_PEER_TLS_ENABLED=true
export ORDERER_CA=${PWD}/organizations/ordererOrganizations/example.com/orderers/orderer.example.com/msp/tlscacerts/tlsca.example.com-cert.pem
PEER0_ORG1_CA=${PWD}/organizations/peerOrganizations/org1.example.com/peers/peer0.org1.example.com/tls/ca.crt

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

setGlobalVarsForOrg1() {
  export CORE_PEER_LOCALMSPID="Org1MSP"
  export CORE_PEER_MSPCONFIGPATH=${PWD}/organizations/peerOrganizations/org1.example.com/users/Admin@org1.example.com/msp
  export CORE_PEER_TLS_ROOTCERT_FILE=$PEER0_ORG1_CA
  export CORE_PEER_ADDRESS=localhost:7051
}

## Preconditions

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

if ! docker network inspect ${DOCKER_NETWORK_NAME} >/dev/null 2>&1; then
  echo "ERROR: Docker network ${DOCKER_NETWORK_NAME} not found."
  echo "Start the base network (scripts/start_network.sh) first."
  exit 1
fi

if ! (exec 3<>/dev/tcp/localhost/7051) 2>/dev/null; then
  echo "ERROR: peer0.org1.example.com is not reachable on localhost:7051."
  echo "Ensure the network is up before running this script."
  exit 1
fi
exec 3<&- 3>&-

setGlobalVarsForOrg1
set -x
peer lifecycle chaincode querycommitted --channelID ${CHANNEL_NAME} --name ${CHAINCODE_NAME} --output json >/dev/null 2>./logs/start_token_nodes_querycommitted_log.txt
res=$?
set +x
if [ ${res} -ne 0 ]; then
  echo "ERROR: ${CHAINCODE_NAME} is not committed on channel '${CHANNEL_NAME}'."
  cat ./logs/start_token_nodes_querycommitted_log.txt
  echo "Run scripts/deployTokenCC.sh first."
  exit 1
fi
echo "SUCCESS: ${CHAINCODE_NAME} is committed on channel '${CHANNEL_NAME}'"
echo

# checkMsp NAME PATH
checkMsp() {
  local name=$1
  local cert_file=$2
  if [ ! -s "${cert_file}" ]; then
    echo "ERROR: ${name} MSP not found or empty (${cert_file})."
    echo "Run scripts/registerTokenIdentities.sh and/or scripts/registerTokenWallets.sh first."
    exit 1
  fi
}

checkMsp "issuer" "${PWD}/token-keys/issuer/msp/signcerts/cert.pem"
checkMsp "auditor" "${PWD}/token-keys/auditor/msp/signcerts/cert.pem"
checkMsp "owner" "${PWD}/token-keys/owner/msp/signcerts/cert.pem"
checkMsp "wallet escrow" "${PWD}/token-keys/wallets/escrow/msp/signcerts/cert.pem"
checkMsp "wallet user1" "${PWD}/token-keys/wallets/user1/msp/signcerts/cert.pem"
checkMsp "wallet user2" "${PWD}/token-keys/wallets/user2/msp/signcerts/cert.pem"
echo "SUCCESS: all node and wallet MSPs are present"
echo

# checkNormalizedKeystore NAME KEYSTORE_DIR
checkNormalizedKeystore() {
  local name=$1
  local keystore_dir=$2
  if [ ! -f "${keystore_dir}/priv_sk" ]; then
    echo "ERROR: ${keystore_dir}/priv_sk not found for ${name}."
    echo "Run scripts/normalizeKeystores.sh (registerTokenWallets.sh already calls it for the wallets)."
    exit 1
  fi
}

checkNormalizedKeystore "issuer" "${PWD}/token-keys/issuer/msp/keystore"
checkNormalizedKeystore "auditor" "${PWD}/token-keys/auditor/msp/keystore"
checkNormalizedKeystore "owner" "${PWD}/token-keys/owner/msp/keystore"
checkNormalizedKeystore "wallet escrow" "${PWD}/token-keys/wallets/escrow/msp/keystore"
checkNormalizedKeystore "wallet user1" "${PWD}/token-keys/wallets/user1/msp/keystore"
checkNormalizedKeystore "wallet user2" "${PWD}/token-keys/wallets/user2/msp/keystore"
echo "SUCCESS: all keystores are normalized to priv_sk"
echo

## NOT part of a normal start - see the header for why wiping the volume
## orphans every token this node holds.
if [ "${RESET}" = "true" ]; then
  echo
  echo "############################################################"
  echo "# WARNING: RESET=true will DELETE the Postgres volume       "
  echo "# (tokendb_data) backing all three Token Service nodes.     "
  echo "#                                                           "
  echo "# This destroys each node's key_store - the private keys for"
  echo "# every recipient identity it has ever been issued. Those   "
  echo "# keys are NOT on the ledger. The chain will survive, but   "
  echo "# every existing token becomes permanently unspendable and  "
  echo "# all balances will read as empty.                          "
  echo "#                                                           "
  echo "# Only correct after regenerating public parameters, or on  "
  echo "# a network you are deliberately starting over.             "
  echo "############################################################"
  echo
  printf "Type 'yes' to confirm: "
  read -r confirm
  if [ "${confirm}" != "yes" ]; then
    echo "Aborted."
    exit 1
  fi
  displayMsg "Resetting Token Service node state (RESET=true)"
  set -x
  docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} down -v >&./logs/start_token_nodes_reset_log.txt
  set +x
  cat ./logs/start_token_nodes_reset_log.txt
  echo
fi

## Plain `up -d --build`, no volume wipe. Postgres comes up first and the
## nodes are gated on its healthcheck; issuer/owner additionally wait for the
## auditor, the p2p bootstrap node.

displayMsg "Building and starting Token Service nodes"
set -x
docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} up -d --build >&./logs/start_token_nodes_log.txt
res=$?
set +x
cat ./logs/start_token_nodes_log.txt
verifyResult $res "Token Service nodes built and started" "Failed to build/start Token Service nodes!"

echo
echo "========= Token Service Nodes Started =========== "
echo
echo "Follow-up commands:"
echo "  docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} ps"
echo "  docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} logs -f auditor.example.com"
echo "  docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} logs -f issuer.example.com"
echo "  docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} logs -f owner.example.com"
echo "  curl http://localhost:9000/healthz   # auditor"
echo "  curl http://localhost:9100/healthz   # issuer"
echo "  curl http://localhost:9200/healthz   # owner"
echo "  docker exec tokendb.example.com psql -U tokensdk -d owner -c '\\dt'   # inspect node tables"
echo "  docker exec tokendb.example.com pg_dump -U tokensdk owner > owner-backup.sql   # BACK UP - see volume note in the compose file"

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
