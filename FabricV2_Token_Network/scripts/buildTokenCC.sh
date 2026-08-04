#!/bin/bash


echo
echo " _____   ___    _  __  _____   _   _ "
echo "|_   _| / _ \  | |/ / | ____| | \ | |"
echo "  | |  | | | | | ' /  |  _|   |  \| |"
echo "  | |  | |_| | | . \  | |___  | |\  |"
echo "  |_|   \___/  |_|\_\ |_____| |_| \_|"
echo
echo "====== Building the Token Chaincode (CCAAS) Image ======"
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

## Preconditions

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker not found on PATH."
  echo "Install Docker before running this script."
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: Docker daemon is not reachable."
  echo "Start Docker and ensure the current user can talk to it before running this script."
  exit 1
fi

# fabtoken1_pp.json, not fabtoken_pp.json - tokengen names it after the driver
# version.
if [ ! -s "${PWD}/token-cc/fabtoken1_pp.json" ]; then
  echo "ERROR: ${PWD}/token-cc/fabtoken1_pp.json not found or empty."
  echo "Run scripts/generateTokenParams.sh first to produce the public parameters."
  exit 1
fi

# The Dockerfile is tracked source and lives in docker/, while the build
# CONTEXT below is token-cc/, which holds only generated artifacts and is
# git-ignored. Splitting them is what lets a fresh clone build this image.
CC_DOCKERFILE="${PWD}/docker/Dockerfile.token-cc"

if [ ! -f "${CC_DOCKERFILE}" ]; then
  echo "ERROR: ${CC_DOCKERFILE} not found."
  exit 1
fi

## Clones and compiles the full Token SDK inside the build, so a cold build
## takes several minutes; later ones reuse Docker's layer cache.

displayMsg "Building token-cc:latest"
set -x
docker build -t token-cc:latest -f "${CC_DOCKERFILE}" ${PWD}/token-cc
res=$?
set +x
verifyResult $res "Built token-cc:latest" "Failed to build token-cc:latest!"

displayMsg "Image details"
docker images token-cc

echo
echo "========= Built the Token Chaincode (CCAAS) Image =========== "

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
