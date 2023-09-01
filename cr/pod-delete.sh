#!/bin/bash

commands=()
pvc_names=$(kubectl get pod -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

# 遍历并打印 PVC 的名称
for pvc_name in $pvc_names; do
  # echo "PVC Name: $pvc_name"
  yaml=$(cat yaml/pod.yaml)
  (
  echo "$yaml" | sed "s|{name}|$pvc_name|g" | kubectl delete -f -
  ) &
  commands+=($!)
done

for pid in ${commands[@]}; do
    wait $pid
done
