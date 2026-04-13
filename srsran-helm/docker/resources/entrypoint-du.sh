#!/bin/bash
#
# Simplified entrypoint for DU
#

set -e

CONFIG_FILE="/etc/config/gnb-config.yml"

echo "Starting DU..."
echo "Pod IP: ${POD_IP}"
echo "Config file: ${CONFIG_FILE}"

# Update bind addresses with Pod IP if needed
if [ "${HOSTNETWORK}" = "false" ] && [ -n "${POD_IP}" ]; then
    echo "Updating bind addresses to use Pod IP: ${POD_IP}"
    sed -i "s/bind_addr: 0.0.0.0/bind_addr: ${POD_IP}/g" ${CONFIG_FILE}
fi

# Create log directory
mkdir -p /tmp/logs

# Start DU
exec srsdu -c ${CONFIG_FILE}
