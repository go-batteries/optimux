#!/bin/bash

# Define a counter to limit the number of attempts
MAX_ATTEMPTS=1000
COUNTER=0

# Loop until there are no processes to kill or the max attempts are reached
while [ $COUNTER -lt $MAX_ATTEMPTS ]; do
    # Find the PIDs of processes matching "hey" and kill them
    PIDS=$(ps aux | grep "hey " | grep -v grep | awk '{print $2}')

    # If no PIDs are found, exit the loop
    if [ -z "$PIDS" ]; then
        echo "No matching processes found. Exiting."
        break
    fi

    echo "killing $PIDS"
    # Kill the processes
    echo "$PIDS" | xargs kill -9
    echo "Killed processes: $PIDS"

    # Increment the counter
    COUNTER=$((COUNTER + 1))
    echo "Attempt $COUNTER completed."
done

# Check if the loop exited due to reaching the max attempts
if [ $COUNTER -ge $MAX_ATTEMPTS ]; then
    echo "Reached maximum attempts ($MAX_ATTEMPTS). Exiting."
fi
