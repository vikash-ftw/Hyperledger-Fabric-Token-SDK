# Hyperledger Fabric v2.5 + Fabric Token SDK

Fabric v2.5.9 network with a Token SDK v0.10.1 service layer (issuer / auditor / owner)
using the `fabtoken` driver, Postgres persistence, and CCAAS chaincode.

## Prerequisites

Docker, Docker Compose, Go 1.25.7.

```bash
./loadFabricDependencies.sh     # downloads bin/, config/, builders/
cd FabricV2_Token_Network       # all scripts below run from here
```

`tokengen` must be on `PATH`, built from the **v0.10.1** tag:

```bash
go install github.com/hyperledger-labs/fabric-token-sdk/cmd/tokengen@v0.10.1

# verify - must print v0.10.1
go version -m "$(command -v tokengen)" | awk '$1=="mod"{print $3}'
```

## Start

Run in order. Each step depends on the previous one.

```bash
# 1. Fabric network
./scripts/start_fabric-ca.sh                  # CAs
./scripts/registerEnroll.sh                   # peer/orderer/admin MSPs
./scripts/start_network.sh                    # 2 orderers, 2 peers, CouchDB
./scripts/createChannel.sh tokenchannel

# 2. Token identities and wallets
./scripts/registerTokenIdentities.sh          # fscissuer, fscauditor, fscowner
./scripts/registerTokenWallets.sh             # escrow, user1, user2 (+ normalizes keystores)
./scripts/registerWalletRegistrar.sh          # narrow CA identity for POST /register

# 3. Token chaincode
./scripts/generateTokenParams.sh              # -> token-cc/fabtoken1_pp.json
./scripts/buildTokenCC.sh                     # -> token-cc:latest (slow on first run)
./scripts/deployTokenCC.sh tokenchannel 1

# 4. Token service nodes
./scripts/startTokenNodes.sh
```

Verify:

```bash
curl http://localhost:9000/healthz   # auditor
curl http://localhost:9100/healthz   # issuer
curl http://localhost:9200/healthz   # owner
```

## API docs

**<http://localhost:8090>** — Swagger UI covering all three nodes, started
alongside them by `startTokenNodes.sh`.


### Restart / stop

```bash
docker-compose -f docker/docker-compose-token-nodes.yaml restart
./scripts/stop_network.sh
./scripts/startTokenNodes.sh          # brings the nodes back with state intact
```

Node state lives in `/var/hyperledger/tokendb_data`, a host bind mount alongside
the peer/orderer/couchdb directories. `down`, `down -v`, container removal and
image rebuilds all leave it alone — **as long as that directory exists, the network
comes back with every wallet, key and balance.**

> **Never delete `/var/hyperledger/tokendb_data`.** It holds each node's key_store
> and the only copy of any wallet registered via `POST /register`; losing it makes
> every token already sent to that node permanently unspendable. Back up with:
> `docker exec tokendb.example.com pg_dump -U tokensdk owner > owner-backup.sql`

## Endpoints

| Node | Port |
|---|---|
| auditor | 9000 |
| issuer | 9100 |
| owner | 9200 |

### Issuer — `:9100`

| Method | Path | Purpose | Body |
|---|---|---|---|
| GET | `/healthz` | liveness | — |
| POST | `/issue` | mint tokens | `tokenType, quantity, recipient, recipientNode, message` |

```bash
curl -X POST localhost:9100/issue -H 'Content-Type: application/json' -d '{
  "tokenType":"ASSET-LAND-001","quantity":100,"recipient":"user1",
  "recipientNode":"owner","message":"initial mint"}'
```

**Token types:** there is no registry or allowlist. A type is a free-form string
created implicitly by the first `/issue` that names it — the call above mints
`ASSET-LAND-001` on the spot. The only rule is non-empty; the public parameters pin
the issuer and auditor certificates, not the set of types.

To see which types exist, read them off the balances — `GET /accounts` on the owner
returns `wallet -> tokenType -> amount`:

```json
{"message":"got balances for all owner wallets",
 "payload":{"user1":{"ASSET-LAND-001":60},"user2":{"ASSET-LAND-001":40},"escrow":{}}}
```

### Owner — `:9200`

| Method | Path | Purpose | Body / Params |
|---|---|---|---|
| GET | `/healthz` | liveness | — |
| POST | `/register` | **register a new owner** | `walletId` (lowercase UUID v4, optional short prefix) |
| POST | `/transfer` | direct transfer | `tokenType, quantity, sender, recipient, recipientNode, message` |
| POST | `/lock` | escrow for a DvP order | `orderId, tokenType, quantity, sender, listingId` |
| POST | `/confirm` | settle order to buyer | `orderId, recipient` |
| POST | `/cancel` | unwind order to sender | `orderId` |
| POST | `/redeem` | burn tokens | `tokenType, quantity, wallet, message` |
| GET | `/accounts` | balances, all owners + types | — |
| GET | `/accounts/{wallet}` | balance, one owner | `?tokenType=` optional (omit for all types) |
| GET | `/accounts/{wallet}/transactions` | history, one owner | — |

```bash
# transfer
curl -X POST localhost:9200/transfer -H 'Content-Type: application/json' -d '{
  "tokenType":"ASSET-LAND-001","quantity":40,"sender":"user1",
  "recipient":"user2","recipientNode":"owner","message":"payment"}'

# DvP: lock -> confirm (or cancel)
curl -X POST localhost:9200/lock -H 'Content-Type: application/json' -d '{
  "orderId":"ord-001","tokenType":"ASSET-LAND-001","quantity":20,
  "sender":"user1","listingId":"listing-abc"}'

curl -X POST localhost:9200/confirm -H 'Content-Type: application/json' \
  -d '{"orderId":"ord-001","recipient":"user2"}'

curl -X POST localhost:9200/cancel -H 'Content-Type: application/json' \
  -d '{"orderId":"ord-001"}'          # returns tokens to the original sender

# register a NEW OWNER - enrolls it with the CA and makes it usable immediately.
# Generated, not hardcoded: a handle is burned on first use, so a fixed one
# works once and returns 400 ever after.
WALLET="usr-$(uuidgen | tr '[:upper:]' '[:lower:]')"
curl -X POST localhost:9200/register -H 'Content-Type: application/json' \
  -d "{\"walletId\":\"$WALLET\"}"
curl localhost:9200/accounts/$WALLET   # usable at once

# redeem / balances / history
curl -X POST localhost:9200/redeem -H 'Content-Type: application/json' \
  -d '{"tokenType":"ASSET-LAND-001","quantity":5,"wallet":"user2","message":"burn"}'
curl localhost:9200/accounts
curl localhost:9200/accounts/user1
curl localhost:9200/accounts/user1/transactions
```

**Owner registration:** `POST /register` creates a new token owner on demand — it
registers and enrolls the identity with the Fabric CA and adds it to the Token SDK,
so the handle is immediately valid as a `sender`/`recipient` in `/transfer`, `/lock`
and `/redeem`. Custodial: this node holds the signing key. The handle is caller-supplied
and permanent — there is no unregister API, so reusing one returns HTTP 400.
The fixed wallets (`escrow`, `user1`, `user2`) come from `registerTokenWallets.sh` instead.

**Transaction history:** every row carries an `operationType` naming the API
operation that produced it, and DvP legs also carry their `orderId`.

| value | meaning |
|---|---|
| `ISSUE` | tokens created — supply up, no sender |
| `TRANSFER` | plain movement between two wallets, final |
| `LOCK` | first DvP leg — into `escrow`, `orderId` claimed |
| `CONFIRM` | second DvP leg — out of `escrow` to the buyer |
| `CANCEL` | second DvP leg — out of `escrow` back to whoever locked it |
| `REDEEM` | tokens destroyed — supply down, no recipient |


### Auditor — `:9000`

Independent view from the auditor's own database.

| Method | Path | Params |
|---|---|---|
| GET | `/healthz` | — |
| GET | `/accounts/{wallet}` | `?tokenType=` **required** |
| GET | `/accounts/{wallet}/transactions` | — |

```bash
curl 'localhost:9000/accounts/user1?tokenType=ASSET-LAND-001'
curl localhost:9000/accounts/user1/transactions
```
