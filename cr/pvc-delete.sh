#!/bin/bash

names=$(kubectl get pvc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
commands=()
# 遍历并打印 PVC 的名称
for name in $names; do
  (
  $(kubectl delete pvc ${name})
  ) &
  commands+=($!)

done

for pid in "${commands[@]}"; do
    wait $pid
done
