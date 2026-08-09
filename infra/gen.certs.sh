#!/bin/bash

HOST="$1"

sudo apt update
sudo apt install -y certbot python3-certbot-nginx

is_ok=$(curl -sI "$HOST" | grep '200 OK')

if [[ -z "$is_ok" ]]; then
  echo "host name is not 200 OK"
  exit 1
fi

sudo systemctl stop nginx
sudo certbot certonly --standalone -d "$HOST"

echo "Update the ssl certificate and key in nginx.conf"
echo "Run:"
echo "sudo nginx -t"
echo "sudo systemctl start nginx"
