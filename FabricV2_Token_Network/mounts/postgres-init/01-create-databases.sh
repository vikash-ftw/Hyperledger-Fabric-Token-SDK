#!/bin/sh
# Create one database per Token Service node.
#
# One DB per node rather than table prefixes in a shared one: the Token SDK
# derives table names from the TMS coordinates, and all three nodes share the
# same network/channel/namespace. In a shared database they would silently
# share key_store, wallets and id_signers - tables holding the private keys
# behind every pseudonymous identity, so that is corruption, not confusion.
# Separate databases also make a mistake loud (`database "owner" does not
# exist`) and allow per-node pg_dump.
#
# The postgres image only runs these scripts when the data directory is EMPTY,
# so editing this file does nothing to an existing volume.
set -e

for db in issuer auditor owner; do
  echo "creating database ${db}..."
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
		CREATE DATABASE "${db}" OWNER "${POSTGRES_USER}";
EOSQL
done

echo "token service databases created"
