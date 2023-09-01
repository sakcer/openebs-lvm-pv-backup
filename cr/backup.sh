#!/bin/bash

commands=()
# 获取所有 PVC 的名称
pvc_names=$(kubectl get pvc --all-namespaces -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

# 遍历并打印 PVC 的名称
for pvc_name in $pvc_names; do
  # echo "PVC Name: $pvc_name"
  yaml=$(cat yaml/backup.yaml)
  (
  echo "$yaml" | sed "s|{PVC}|$pvc_name|g" | kubectl apply -f -
  ) &

  commands+=($!)
done

for pid in "${commands[@]}"; do
  wait $pid
done
