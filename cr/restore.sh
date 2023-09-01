#!/bin/bash

commands=()
pvc_names=$(kubectl get backups.br.sealos.io.sealos.io --all-namespaces -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

for pvc_name in $pvc_names; do
  # echo "PVC Name: $pvc_name"
  yaml=$(cat yaml/restore.yaml)
  (
  echo "$yaml" | sed "s|{PVC}|$pvc_name|g" | kubectl apply -f -
  ) &
  
  commands+=($!)
done

for pid in "${commands[@]}"; do
  wait $pid
done
