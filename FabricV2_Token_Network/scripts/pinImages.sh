#!/bin/bash
# Re-pin every external image reference to an immutable digest.
#
# Tags are mutable. `golang:1.25` moved from 1.25.7 to 1.25.13 with no change in
# this repository, silently changing the compiler that built the node binaries -
# the kind of drift that makes a build irreproducible without ever failing.
#
# Versions stay declared in .env (compose) or literally in the Dockerfiles; this
# script only derives the digest that goes with them. Run it after bumping a
# version, and review the diff - that diff IS the record of what changed.
#
# Requires network access. Reads nothing it does not also rewrite in place.

set -e

cd "$(dirname "$0")/.."

echo
echo "====== Re-pinning image digests ======"
echo

if ! docker buildx version >/dev/null 2>&1; then
  echo "ERROR: docker buildx not available; cannot resolve digests."
  exit 1
fi

# shellcheck disable=SC1091
. ./.env

# resolve REF -> sha256:...  (queries the registry, does not pull)
resolve() {
  docker buildx imagetools inspect "$1" 2>/dev/null | awk '/^Digest:/{print $2; exit}'
}

# pin FILE WRITTEN_REF [RESOLVABLE_REF]
#
# WRITTEN_REF is what appears in the file, which in the compose files goes
# through $IMAGE_TAG_* rather than a literal tag. RESOLVABLE_REF is the expanded
# form the registry understands; it defaults to WRITTEN_REF.
#
# Only FROM and image: lines are touched. Rewriting every occurrence would also
# corrupt the prose in comments that name these images.
pin() {
  local file=$1 written=$2 resolvable=${3:-$2} digest escaped
  digest=$(resolve "$resolvable")
  if [ -z "${digest}" ]; then
    echo "  FAILED to resolve ${resolvable}"
    return 1
  fi
  escaped=$(printf '%s' "$written" | sed 's/[.[\*^$/]/\\&/g')
  sed -i -E "/^(FROM |[[:space:]]*image:)/ s|${escaped}(@sha256:[a-f0-9]{64})?|${written}@${digest}|g" "$file"
  printf '  %-52s %s\n' "$written" "$digest"
}

# Scope is deliberately narrow: only the two images we BUILD - the token
# chaincode and the three Fabric client nodes. Their bases decide what compiler
# and what libc produced the binaries that hold keys and move tokens, and both
# are rolling tags: golang:1.25 moved from 1.25.7 to 1.25.13 with no change in
# this repository.
#
# Everything else is pulled, not built, and stays on a plain version tag.
# Pinning those was churn without a matching risk - and where the version lives
# in .env as $IMAGE_TAG_*, a digest is actively harmful: Docker resolves
# `tag@digest` by the digest, so bumping the tag without re-running this script
# would silently keep running the old image while .env advertised the new one.
pin docker/Dockerfile.nodes    "golang:1.25"
pin docker/Dockerfile.nodes    "debian:trixie-slim"
pin docker/Dockerfile.token-cc "golang:1.25"
pin docker/Dockerfile.token-cc "debian:trixie-slim"

echo
echo "========= Digests re-pinned - review with git diff ========= "
echo
