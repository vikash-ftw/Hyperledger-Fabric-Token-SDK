#!/bin/bash

# Path to .env file
ENV_FILE=${PWD}/.env

# use this as the default docker-compose yaml definition
COMPOSE_FILE_BASE=${PWD}/docker/docker-compose-net.yaml
# certificate authorities compose file
COMPOSE_FILE_CA=${PWD}/docker/docker-compose-ca.yaml
# default database
DATABASE="couchdb"
# if database is couchdb then couch compose file
COMPOSE_FILE_COUCH=${PWD}/docker/docker-compose-couch.yaml
# token chaincode (CCAAS) compose file
COMPOSE_FILE_TOKEN_CC=${PWD}/docker/docker-compose-token-cc.yaml
# token service nodes compose file
COMPOSE_FILE_TOKEN_NODES=${PWD}/docker/docker-compose-token-nodes.yaml

COMPOSE_FILES="-f ${COMPOSE_FILE_CA} -f ${COMPOSE_FILE_BASE}"

if [ "${DATABASE}" == "couchdb" ]; then
    COMPOSE_FILES="${COMPOSE_FILES} -f ${COMPOSE_FILE_COUCH}"
fi

# Token containers first - they attach to the fbn network created by the base
# compose file. startTokenNodes.sh brings the nodes up without --env-file, so
# they must come down the same way or compose matches no containers.
docker-compose -f ${COMPOSE_FILE_TOKEN_NODES} down --volumes --remove-orphans

docker-compose --env-file $ENV_FILE -f ${COMPOSE_FILE_TOKEN_CC} down --volumes

docker-compose --env-file $ENV_FILE ${COMPOSE_FILES} down --volumes --remove-orphans