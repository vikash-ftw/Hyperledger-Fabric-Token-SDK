#!/bin/bash

# Path to .env file
ENV_FILE=${PWD}/.env

# certificate authorities compose file
COMPOSE_FILE_CA=${PWD}/docker/docker-compose-ca.yaml

COMPOSE_FILES="-f ${COMPOSE_FILE_CA}"

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: 'docker compose' (Compose v2 plugin) not found."
  echo "Install/update Docker so the compose plugin is available."
  exit 1
fi

docker compose --env-file $ENV_FILE ${COMPOSE_FILES} up -d 2>&1

if [ $? -ne 0 ]; then
    echo "ERROR !!!! Unable to start fabric-ca"
    exit 1
fi
docker ps