#!/bin/bash

sudo yum update --security

echo "net.core.somaxconn=4096" | sudo tee -a /etc/sysctl.conf
# echo 300000 | sudo tee /proc/sys/fs/nr_open

mkdir -p /etc/ecs

echo "ECS_CLUSTER=${ECS_CLUSTER_NAME}" >> /etc/ecs/ecs.config
echo "ECS_CONTAINER_INSTANCE_PROPAGATE_TAGS_FROM=ec2_instance" >> /etc/ecs/ecs.config
echo "ECS_ENABLE_CONTAINER_METADATA=true" >> /etc/ecs/ecs.config

echo "ECS_LOGFILE=/log/ecs-agent.log" >> /etc/ecs/ecs.config


NETWORK_NAME="optimux-net"


function mount_partition() {
  DEVICE="$1"
  MOUNT_POINT="$2"

  while [ ! -b "$DEVICE" ]; do
    echo "Waiting for EBS volume..."
    sleep 5
  done

  # Format if unformatted
  if [ -z "$(blkid $DEVICE)" ]; then
    echo "Formatting $DEVICE..."
    mkfs -t ext4 $DEVICE
  fi

  mkdir -p $MOUNT_POINT

  # Mount volume
  mount $DEVICE $MOUNT_POINT

  # Persist across reboots
  grep -qxF "$DEVICE $MOUNT_POINT ext4 defaults,nofail 0 2" /etc/fstab || echo "$DEVICE $MOUNT_POINT ext4 defaults 0 0" >> /etc/fstab
  # echo "$DEVICE $MOUNT_POINT ext4 defaults,nofail 0 2" >> /etc/fstab
}

function wait_for_app_up() {
  app_name="$1"
  found=""

  echo "Waiting for $app_name container to be ready..." >> /tmp/ecs.err.log

  for i in {1..200}; do
    APP_ID=$(docker ps --format '{{.ID}} {{.Names}}' | grep "$app_name-[^-]*$" | awk '{print $1}')

    if [[ ! -z "$APP_ID" ]]; then
      APP_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$APP_ID")
      echo "✅ Found $app_name at $APP_IP" >> /tmp/ecs.err.log

      if ! docker network inspect "$NETWORK_NAME" | grep -q "$APP_ID"; then
        echo "🔗 Attaching $app_name ($APP_ID) to $NETWORK_NAME" >> /tmp/ecs.err.log
        # echo "$APP_IP $app_name.local" >> /etc/hosts
        docker network connect "$NETWORK_NAME" "$APP_ID" >> /tmp/ecs.err.log 2>&1
      else
        echo "✔ $app_name is already attached to $NETWORK_NAME" >> /tmp/ecs.err.log
      fi

      found="1"
      break
    fi

    sleep 2
  done

  if [[ -z "$found" ]]; then
    echo "❌ service discovery failed for $app_name" >> /tmp/ecs.err.log
  fi
}

function setup_docker_network() {
  docker network inspect "$NETWORK_NAME" >/dev/null 2>&1 || \
    docker network create "$NETWORK_NAME"
}

pip3 install awscli

setup_docker_network

# wait_for_app_up "${APP_NAME}" &
# pid1=$!

#wait_for_app_up "${DD_APP_NAME}" &
# pid2=$!

# mount_partition "/dev/xvdf" "/mnt/log"
# mount_partition "/dev/xvdg" "/mnt/cache"

mkdir -p /tmp/datadog-agent/conf.d/
aws s3 cp "${REMOTE_PROBES_CFG_DIR}" /tmp/datadog-agent/conf.d/ --recursive

docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -d optimux/ecsdiscovery:${ECS_DISCOVERY_TAG} \
  --containers="${APP_NAME},${DD_APP_NAME}" \
  --network=optimux-net \
  --timeout=30m

