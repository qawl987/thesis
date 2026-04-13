#!/bin/bash
#
# Simplified entrypoint for CU-CP
#

set -e

CONFIG_FILE="/etc/config/gnb-config.yml"

echo "Starting CU-CP..."
echo "Pod IP: ${POD_IP}"
echo "Config file: ${CONFIG_FILE}"

# Update bind addresses with Pod IP if needed
if [ "${HOSTNETWORK}" = "false" ] && [ -n "${POD_IP}" ]; then
    echo "Updating bind addresses to use Pod IP: ${POD_IP}"
    sed -i "s/bind_addr: 0.0.0.0/bind_addr: ${POD_IP}/g" ${CONFIG_FILE}
fi

# Create log directory
mkdir -p /tmp/logs

# Start CU-CP
exec srscucp -c ${CONFIG_FILE}
