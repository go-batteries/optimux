#!/bin/bash

# Customize your params here
RPGURL="$READ_PG_URL"
PGURL="$PG_URL"
LIMIT=100
VTHREADS=500

# Total days to go back
TOTAL_DAYS=30
# Batch step (in days)
STEP=3

if [[ -z "$RPGURL" ]]; then
    echo "read pg url missing"
    exit 1
fi

if [[ -z "$PGURL" ]]; then
    echo "write pg url missing"
    exit 1
fi

mkdir -p logs

run_batch() {
  local SINCE=$1
  local TO=$2
  local LOG="logs/batch_${SINCE}_${TO}"

  echo "Running batch: $SINCE to $TO days ago"
  ./mediamigrator \
    -rpgurl="$RPGURL" \
    -pgurl="$PGURL" \
    -since="$SINCE" \
    -to="$TO" \
    -limit="$LIMIT" \
    -vthreads="$VTHREADS" > "${LOG}.out.log" 2> "${LOG}.err.log" &
}

# Start from most recent offset
TO=0

while [ "$TO" -lt "$TOTAL_DAYS" ]; do
  SINCE1=$((TO + STEP))
  SINCE2=$((SINCE1 + STEP))
  SINCE3=$((SINCE2 + STEP))
  TO1=$TO
  TO2=$((TO + STEP))
  TO3=$((TO + STEP * 2))

  # Run 3 batches in parallel
  run_batch "$SINCE1" "$TO1"
  run_batch "$SINCE2" "$TO2"
  run_batch "$SINCE3" "$TO3"

  wait  # Wait for all 3 to finish

  # Move to the next window
  TO=$((TO + STEP * 3))
done

