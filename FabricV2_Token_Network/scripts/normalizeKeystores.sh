#!/bin/bash
# normalize every MSP keystore under token-keys/ to a fixed key filename
#
# fabric-ca-client names each key after a hash of itself, different on every
# (re-)enrollment, but the FSC node reads fsc.identity.key.file by EXACT path -
# no directory scan, no glob. Renaming to priv_sk is what lets core.yaml
# hardcode a path that survives re-enrollment.
#
# Run after registerTokenIdentities.sh and registerTokenWallets.sh (which calls
# this itself).

echo
echo " _____   ___    _  __  _____   _   _ "
echo "|_   _| / _ \  | |/ / | ____| | \ | |"
echo "  | |  | | | | | ' /  |  _|   |  \| |"
echo "  | |  | |_| | | . \  | |___  | |\  |"
echo "  |_|   \___/  |_|\_\ |_____| |_| \_|"
echo
echo "====== Normalizing Token SDK MSP Keystores to priv_sk ======"
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

if [ ! -d "${PWD}/token-keys" ]; then
  echo "ERROR: ${PWD}/token-keys not found."
  echo "Run scripts/registerTokenIdentities.sh first to enroll the Token SDK identities."
  exit 1
fi

# Safe to re-run: with priv_sk already present there is nothing to do.
function normalizeKeystoreDir() {
  local keystore_dir=$1

  # priv_sk also matches "*_sk", so it must be excluded or a second run would
  # rename it onto itself.
  local hash_files=()
  for f in "${keystore_dir}"/*_sk; do
    [ -e "${f}" ] || continue
    [ "$(basename "${f}")" = "priv_sk" ] && continue
    hash_files+=("${f}")
  done

  if [ ${#hash_files[@]} -eq 0 ]; then
    if [ -f "${keystore_dir}/priv_sk" ]; then
      echo "SUCCESS: ${keystore_dir} already normalized, skipping"
      echo
    else
      echo "ERROR: no key file found in ${keystore_dir} (neither a hash-named key nor priv_sk)."
      exit 1
    fi
    return
  fi

  if [ ${#hash_files[@]} -gt 1 ]; then
    echo "ERROR: ${keystore_dir} contains more than one candidate key file: ${hash_files[*]}"
    echo "Refusing to guess which one is current - clean up the keystore and re-run."
    exit 1
  fi

  displayMsg "Normalizing ${keystore_dir}"
  set -x
  mv -f "${hash_files[0]}" "${keystore_dir}/priv_sk"
  res=$?
  set +x
  verifyResult $res "Renamed $(basename "${hash_files[0]}") to priv_sk in ${keystore_dir}" "Failed to rename key in ${keystore_dir}!"
}

## Both the three node identities and any wallet identities.
found_any=0
while IFS= read -r -d '' keystore_dir; do
  found_any=1
  normalizeKeystoreDir "${keystore_dir}"
done < <(find "${PWD}/token-keys" -type d -path "*/msp/keystore" -print0 | sort -z)

if [ ${found_any} -eq 0 ]; then
  echo "WARNING: no msp/keystore directories found under ${PWD}/token-keys."
fi

echo
echo "========= Normalized Token SDK MSP Keystores =========== "

echo
echo " _____   _   _   ____   "
echo "| ____| | \ | | |  _ \  "
echo "|  _|   |  \| | | | | | "
echo "| |___  | |\  | | |_| | "
echo "|_____| |_| \_| |____/  "
echo

exit 0
