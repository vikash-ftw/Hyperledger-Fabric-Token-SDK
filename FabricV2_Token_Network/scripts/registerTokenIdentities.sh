#!/bin/bash


echo
echo " _____   ___    _  __  _____   _   _ "
echo "|_   _| / _ \  | |/ / | ____| | \ | |"
echo "  | |  | | | | | ' /  |  _|   |  \| |"
echo "  | |  | |_| | | . \  | |___  | |\  |"
echo "  |_|   \___/  |_|\_\ |_____| |_| \_|"
echo
echo "====== Registering and Enrolling Token SDK Identities ======"
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

if [ ! -d "${PWD}/organizations/peerOrganizations/org1.example.com" ]; then
  echo "ERROR: ${PWD}/organizations/peerOrganizations/org1.example.com not found."
  echo "Run scripts/registerEnroll.sh first to enroll the CA admin and bootstrap Org1 crypto material."
  exit 1
fi

if ! (exec 3<>/dev/tcp/localhost/7054) 2>/dev/null; then
  echo "ERROR: Cannot reach Fabric CA (ca-org1) at localhost:7054."
  echo "Ensure the network and CA containers are up before running this script."
  exit 1
fi
exec 3<&- 3>&-

## CA-admin context (the admin identity enrolled by registerEnroll.sh)
export FABRIC_CA_CLIENT_HOME=${PWD}/organizations/peerOrganizations/org1.example.com/

function registerAndEnrollTokenIdentity() {
  local id_name=$1
  local id_secret=$2
  local msp_dir=$3

  displayMsg "Registering ${id_name}"
  set -x
  fabric-ca-client identity list --caname ca-org1 --id "${id_name}" --tls.certfiles ${PWD}/organizations/fabric-ca/org1/tls-cert.pem >/dev/null 2>&1
  exists=$?
  set +x

  if [ ${exists} -eq 0 ]; then
    echo "SUCCESS: Identity ${id_name} is already registered, skipping registration"
    echo
  else
    set -x
    fabric-ca-client register --caname ca-org1 --id.name "${id_name}" --id.secret "${id_secret}" --id.type client --tls.certfiles ${PWD}/organizations/fabric-ca/org1/tls-cert.pem
    res=$?
    set +x
    if [ ${res} -eq 0 ]; then
      echo "SUCCESS: Registered ${id_name}"
    else
      echo "WARNING: Register ${id_name} reported an error (identity may already exist) - continuing to enroll"
    fi
    echo
  fi

  displayMsg "Generating the ${id_name} msp"
  mkdir -p "${msp_dir}"
  set -x
  fabric-ca-client enroll -u https://${id_name}:${id_secret}@localhost:7054 --caname ca-org1 -M "${msp_dir}" --tls.certfiles ${PWD}/organizations/fabric-ca/org1/tls-cert.pem
  res=$?
  set +x
  verifyResult $res "Generated ${id_name} msp" "Failed to generate ${id_name} msp!"

  cp ${PWD}/organizations/peerOrganizations/org1.example.com/msp/config.yaml "${msp_dir}/config.yaml"
}

TOKEN_ID_NAMES=(fscissuer fscauditor fscowner)
TOKEN_ID_SECRETS=(fscissuerpw fscauditorpw fscownerpw)
TOKEN_ID_DIRS=(issuer auditor owner)

for i in "${!TOKEN_ID_NAMES[@]}"; do
  registerAndEnrollTokenIdentity \
    "${TOKEN_ID_NAMES[$i]}" \
    "${TOKEN_ID_SECRETS[$i]}" \
    "${PWD}/token-keys/${TOKEN_ID_DIRS[$i]}/msp"
done

echo
echo "========= Generated Token SDK Identities =========== "

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
