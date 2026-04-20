#!/bin/bash
# Start tinyproxy and apply iptables egress rules. Invoked as root by the
# container entrypoint at PID-1 startup; not callable from inside the
# container by claude (no sudo).
set -e

if [ "$(id -u)" -ne 0 ]; then
    echo "init-firewall.sh must run as root" >&2
    exit 1
fi

mkdir -p /run/tinyproxy /var/log/tinyproxy
chown tinyproxy:tinyproxy /run/tinyproxy /var/log/tinyproxy

# Re-exec safe: kill any stale instance before starting
pkill -u tinyproxy tinyproxy 2>/dev/null || true
runuser -u tinyproxy -- tinyproxy -c /etc/tinyproxy/tinyproxy.conf

# Egress: deny all, allow loopback, allow tinyproxy (the only path out for
# claude), allow dev (operator's unrestricted shell).
iptables -F OUTPUT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A OUTPUT -m owner --uid-owner tinyproxy -j ACCEPT
iptables -A OUTPUT -m owner --uid-owner dev -j ACCEPT
iptables -P OUTPUT DROP

echo "tusk-devcontainer: firewall + tinyproxy ready"
