# Token Service - issuer node
#
# The build context MUST be token-service/ (the workspace root), not
# token-service/issuer/: this module imports the local `views` module, which
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

RUN CGO_ENABLED=1 go build -buildvcs=false -o /issuer ./issuer && chmod +x /issuer

# debian:trixie-slim is the OS golang:1.25 itself is built on, so glibc matches
# exactly for the cgo-linked binary without shipping the Go toolchain.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=builder /issuer /usr/local/bin/issuer
ENV CONF_DIR=/var/fsc/conf
EXPOSE 9100
EXPOSE 9101
CMD ["issuer"]
