#!/bin/bash

HOST="10.147.20.219"

ulimit -n 6555536

b1() {

	SIZES=("360x240", "120x240")
	# IMAGES=("sample.jpg" "pexels.jpg")
	IMAGES=("https://images.unsplash.com/photo-1503342394128-c104d54dba01?ixlib=rb-4.0.3&q=85&fm=jpg&crop=entropy&cs=srgb&dl=ian-dooley-8HqPXTToMn0-unsplash.jpg")

	for i in {1..100}; do
		IMAGE=${IMAGES[$RANDOM % ${#IMAGES[@]}]}
		SIZE=${SIZES[$RANDOM % ${#SIZES[@]}]}

		echo "Testing: $IMAGE with size $SIZE"
		
		# wrk -t1 -c1 -d5s "http://${HOST}:8811/resize?image_url=${IMAGE}&format=webp&sizes=${SIZE}" | tee -a "results/results_$(basename "$IMAGE")_${SIZE}.txt"
		hey -c 5 -n 10 "http://${HOST}:8811/resize?image_url=${IMAGE}&format=webp&sizes=${SIZE}" -h2 > "results/results_$(basename "$IMAGE")_${SIZE}_hn10c5.txt"
		# wrk -t2 -c2 -d5s "http://$HOST:8811/resize?image=$IMAGE&format=webp&sizes=$SIZE" | tee -a "results/results_$IMAGE_$SIZE.txt"
		# wrk -t2 -c10 -d5s "http://$HOST:8811/resize?image=$IMAGE&format=webp&sizes=$SIZE" | tee -a "results/results_$IMAGE_$SIZE.txt"
	done
}

b2() {
	SIZES=("800x600" "640x480" "360x420" "480x0")
	IMAGES=("sample.jpg" "pexels.jpg")

	for i in {1..100}; do
		IMAGE=${IMAGES[$RANDOM % ${#IMAGES[@]}]}

		# Pick 2-4 random sizes
		NUM_SIZES=$(( RANDOM % 3 + 2 ))  # Picks 2, 3, or 4 sizes
		SELECTED_SIZES=()

		while [[ ${#SELECTED_SIZES[@]} -lt $NUM_SIZES ]]; do
			SIZE=${SIZES[$RANDOM % ${#SIZES[@]}]}
			if [[ ! " ${SELECTED_SIZES[@]} " =~ " $SIZE " ]]; then
				SELECTED_SIZES+=("$SIZE")
			fi
		done

		# Convert array to comma-separated string
		SIZE_PARAM=$(IFS=,; echo "${SELECTED_SIZES[*]}")

		echo "Testing: $IMAGE with sizes=$SIZE_PARAM"
		wrk -t4 -c50 -d30s "http://$HOST:8811/resize?image=$IMAGE&format=jpeg&sizes=$SIZE_PARAM"
	done
}

b1
