#!/bin/bash
# PID 1 of the dev container. Runs as root, applies the egress firewall,
# then exec's the long-running CMD (sleep infinity). All user shells are
# attached afterward via `docker exec -u <user>`, by which time the
# firewall is already in place.
set -e

if [ "$(id -u)" -ne 0 ]; then
    echo "entrypoint must run as root (got uid $(id -u))" >&2
    exit 1
fi

/usr/local/bin/init-firewall.sh

exec "$@"
