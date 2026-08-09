#!/bin/bash

set -e

# Mount tmpfs for caching
echo "🗄️ Creating the directories..."
# Make sure tmpfs exists

mkdir -p /tmp/shm/image_cache
mkdir -p /tmp/shm/edge_cache
mkdir -p /var/log/nginx

# sysctl -w net.core.somaxconn=4096

# mount -t tmpfs -o size=1G tmpfs /tmp/shm/image_cache
# mount -t tmpfs -o size=1G tmpfs /tmp/shm/edge_cache

echo "⏳ Waiting for nginx and go server to start..."

/usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf

