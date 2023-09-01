#!/bin/bash

commands=()

pod_names=$(kubectl get pod -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

for pod_name in $pod_names; do
  file=$(cat /dev/urandom | tr -dc 'a-z0-9' | fold -w 5 | head -n 1)
  random=$(( RANDOM % 50 ))
  (
    echo ${pod_name} ${file} $(kubectl exec ${pod_name} -- sh -c "head -c ${random}m /dev/urandom > /data/${file}")
  ) &

  commands+=($!)
done

for pid in "${commands[@]}"; do
  wait $pid
done
