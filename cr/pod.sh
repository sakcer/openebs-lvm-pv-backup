#!/bin/bash
commands=()

for ((i=1; i<=20; i++)); do
  # name="pod-"$(cat /dev/urandom | tr -dc 'a-z0-9' | fold -w 5 | head -n 1)
  name="pod-"$i
  yaml=$(cat yaml/pod.yaml)
  (
  echo "$yaml" | sed "s|{name}|$name|g" | kubectl apply -f -
  ) &
  commands+=($!)
done

for pid in ${commands[@]}; do
    wait $pid
done

