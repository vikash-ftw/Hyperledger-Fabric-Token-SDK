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
```

## Start

Run in order. Each step depends on the previous one.

```bash
# 1. Fabric network
./scripts/start_fabric-ca.sh                  # CAs
./scripts/registerEnroll.sh                   # peer/orderer/admin MSPs
./scripts/start_network.sh                    # 2 orderers, 2 peers, CouchDB
./scripts/createChannel.sh samplechannel

# 2. Token identities and wallets
./scripts/registerTokenIdentities.sh          # fscissuer, fscauditor, fscowner
./scripts/registerTokenWallets.sh             # escrow, user1, user2 (+ normalizes keystores)
./scripts/registerWalletRegistrar.sh          # narrow CA identity for POST /wallets

# 3. Token chaincode
./scripts/generateTokenParams.sh              # -> token-cc/fabtoken1_pp.json
./scripts/buildTokenCC.sh                     # -> token-cc:latest (slow on first run)
./scripts/deployTokenCC.sh samplechannel 1

# 4. Token service nodes
./scripts/startTokenNodes.sh
```

Verify:

```bash
curl http://localhost:9000/healthz   # auditor
curl http://localhost:9100/healthz   # issuer
curl http://localhost:9200/healthz   # owner
```

### Restart / stop

```bash
docker-compose -f docker/docker-compose-token-nodes.yaml restart   # safe
./scripts/stop_network.sh
```

> **Never run `down -v` on `docker-compose-token-nodes.yaml`.** The `tokendb_data`
> volume holds each node's key_store; deleting it makes every token already sent to
> that node permanently unspendable. Use `RESET=true ./scripts/startTokenNodes.sh`
> if you genuinely need a clean slate. Back up with:
> `docker exec tokendb.example.com pg_dump -U tokensdk owner > owner-backup.sql`

## Endpoints

| Node | Port |
|---|---|
| auditor | 9000 |
| issuer | 9100 |
| owner | 9200 |

### Issuer — `:9100`

| Method | Path | Body |
|---|---|---|
| GET | `/healthz` | — |
| POST | `/issue` | `tokenType, quantity, recipient, recipientNode, message` |

```bash
curl -X POST localhost:9100/issue -H 'Content-Type: application/json' -d '{
  "tokenType":"USD","quantity":100,"recipient":"user1",
  "recipientNode":"owner","message":"initial mint"}'
```

### Owner — `:9200`

| Method | Path | Purpose | Body / Params |
|---|---|---|---|
| GET | `/healthz` | liveness | — |
| POST | `/wallets` | **register a new owner** | `walletId` (lowercase UUID v4, optional short prefix) |
| POST | `/transfer` | direct transfer | `tokenType, quantity, sender, recipient, recipientNode, message` |
| POST | `/lock` | escrow for a DvP order | `orderId, tokenType, quantity, sender, listingId` |
| POST | `/confirm` | settle order to buyer | `orderId, recipient` |
| POST | `/cancel` | unwind order to sender | `orderId` |
| POST | `/redeem` | burn tokens | `tokenType, quantity, wallet, message` |
| GET | `/accounts` | balances, all owners | — |
| GET | `/accounts/{wallet}` | balance, one owner | `?tokenType=` optional |
| GET | `/accounts/{wallet}/transactions` | history, one owner | — |

```bash
# transfer
curl -X POST localhost:9200/transfer -H 'Content-Type: application/json' -d '{
  "tokenType":"USD","quantity":40,"sender":"user1",
  "recipient":"user2","recipientNode":"owner","message":"payment"}'

# DvP: lock -> confirm (or cancel)
curl -X POST localhost:9200/lock -H 'Content-Type: application/json' -d '{
  "orderId":"ord-001","tokenType":"USD","quantity":20,
  "sender":"user1","listingId":"listing-abc"}'

curl -X POST localhost:9200/confirm -H 'Content-Type: application/json' \
  -d '{"orderId":"ord-001","recipient":"user2"}'

curl -X POST localhost:9200/cancel -H 'Content-Type: application/json' \
  -d '{"orderId":"ord-001"}'          # returns tokens to the original sender

# register a NEW OWNER - enrolls it with the CA and makes it usable immediately
curl -X POST localhost:9200/wallets -H 'Content-Type: application/json' \
  -d '{"walletId":"usr-3f8a1c22-9d4e-4b17-8c65-2ab7e91f0d34"}'
curl localhost:9200/accounts/usr-3f8a1c22-9d4e-4b17-8c65-2ab7e91f0d34   # usable at once

# redeem / balances / history
curl -X POST localhost:9200/redeem -H 'Content-Type: application/json' \
  -d '{"tokenType":"USD","quantity":5,"wallet":"user2","message":"burn"}'
curl localhost:9200/accounts
curl localhost:9200/accounts/user1
curl localhost:9200/accounts/user1/transactions
```

**Owner registration:** `POST /wallets` creates a new token owner on demand — it
registers and enrolls the identity with the Fabric CA and adds it to the Token SDK,
so the handle is immediately valid as a `sender`/`recipient` in `/transfer`, `/lock`
and `/redeem`. Custodial: this node holds the signing key. The handle is caller-supplied
and permanent — there is no unregister API, so reusing one returns HTTP 400.
The fixed wallets (`escrow`, `user1`, `user2`) come from `registerTokenWallets.sh` instead.

**DvP semantics:** `/lock` moves tokens `sender -> escrow` and claims `orderId`.
`/confirm` settles `escrow -> recipient`; `/cancel` returns them to the original
sender (no recipient parameter). Each `orderId` is single-use; reuse, or settling
an order that is not `locked`, returns HTTP 400. Order state lives in the
`app_order_locks` table in the owner database.

> `/confirm` and `/cancel` are **not yet authenticated** — `requireAuthority()` in
> `token-service/owner/dvp.go` is a no-op stub. Anyone who can reach port 9200 can
> settle or cancel any locked order.

### Auditor — `:9000`

Independent view from the auditor's own database.

| Method | Path | Params |
|---|---|---|
| GET | `/healthz` | — |
| GET | `/accounts/{wallet}` | `?tokenType=` **required** |
| GET | `/accounts/{wallet}/transactions` | — |

```bash
curl 'localhost:9000/accounts/user1?tokenType=USD'
curl localhost:9000/accounts/user1/transactions
```
