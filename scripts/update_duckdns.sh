
#!/bin/bash

# Your DuckDNS subdomain and token
DUCKDNS_DOMAIN="$DUCK_DOMAIN"
DUCKDNS_TOKEN="$DUCK_TOKEN"

# Get the current public IP
CURRENT_IP=$(curl -s https://api64.ipify.org)

# Update DuckDNS with the current IP
RESPONSE=$(curl -s "https://www.duckdns.org/update?domains=$DUCKDNS_DOMAIN&token=$DUCKDNS_TOKEN&ip=$CURRENT_IP")

# Check if the update was successful
if [ "$RESPONSE" = "OK" ]; then
  echo "$(date) - DuckDNS update successful. IP: $CURRENT_IP"
else
  echo "$(date) - DuckDNS update failed. Response: $RESPONSE"
fi
