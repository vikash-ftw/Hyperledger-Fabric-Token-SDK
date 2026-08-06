# Token Service - owner node
#
# The build context MUST be token-service/ (the workspace root), not
# token-service/owner/: this module imports the local `views` module, which
# only resolves through go.work, and go.work lists all four modules - a missing
# one fails with "cannot load module X listed in go.work file".
#
# Never set GOWORK=off. The workspace's unified build list is what keeps all
# four modules on the same gnark-crypto/quic-go/libp2p versions; per-module MVS
# drift previously produced an unresolvable linker error against IBM/mathlib.
#
# golang:1.25 in both stages: both SDKs declare `go 1.25.7`.

FROM golang:1.25 AS builder
WORKDIR /workspace

COPY go.work go.work.sum ./
COPY issuer/ ./issuer/
COPY auditor/ ./auditor/
COPY owner/ ./owner/
COPY views/ ./views/

RUN CGO_ENABLED=1 go build -buildvcs=false -o /owner ./owner && chmod +x /owner

# fabric-ca-client is needed at RUNTIME by POST /wallets, which shells out to it
# (owner/wallets.go). Downloaded in its own stage because the slim runtime below
# has no curl. Version must match the fabric-ca server (IMAGE_TAG_CA in ../.env).
#
# Shelling out rather than importing github.com/hyperledger/fabric-ca: the SDK
# ships no Go client, and that module would drag a large tree into the pinned
# go.work build list.
FROM golang:1.25 AS cadl
ARG FABRIC_CA_VERSION=1.5.12
RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    curl -fsSL -o /tmp/ca.tgz \
      "https://github.com/hyperledger/fabric-ca/releases/download/v${FABRIC_CA_VERSION}/hyperledger-fabric-ca-linux-${arch}-${FABRIC_CA_VERSION}.tar.gz"; \
    tar -xzf /tmp/ca.tgz -C /usr/local bin/fabric-ca-client; \
    rm -f /tmp/ca.tgz

# debian:trixie-slim is the OS golang:1.25 itself is built on, so glibc matches
# exactly for the cgo-linked binary without shipping the Go toolchain.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=cadl /usr/local/bin/fabric-ca-client /usr/local/bin/fabric-ca-client
COPY --from=builder /owner /usr/local/bin/owner
ENV CONF_DIR=/var/fsc/conf
EXPOSE 9200
EXPOSE 9201
CMD ["owner"]
