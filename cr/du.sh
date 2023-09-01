#!/bin/bash

pod_names=$(kubectl get pod -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

# for pod_name in $pod_names; do
#   echo ${pod_name} $(kubectl exec -it ${pod_name} -- du /data -h)
# done

commands=()

for pod_name in $pod_names; do
  (
    echo $pod_name $(kubectl exec $pod_name -- du /data -h -d1)
  ) &

  commands+=($!)
done

for pid in "${commands[@]}"; do
  wait $pid
done
