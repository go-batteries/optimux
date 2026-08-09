#!/bin/sh

CACHE_DIR="/mnt/cache"
THRESHOLD=80  # Set disk usage limit (80%)
DELETE_OLDER_THAN=3 # Days

while true; do
    USAGE=$(df "$CACHE_DIR" | awk 'NR==2 {print $5}' | sed 's/%//')

    if [ "$USAGE" -gt "$THRESHOLD" ]; then
        echo "Disk usage ($USAGE%) exceeded $THRESHOLD%. Deleting files older than $DELETE_OLDER_THAN days..."
        find "$CACHE_DIR" -type f -mtime +$DELETE_OLDER_THAN -delete
    fi

    sleep 6000  # Run every 100 minutes
done
