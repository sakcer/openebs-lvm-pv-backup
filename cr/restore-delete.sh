#!/bin/bash

pvc_names=$(kubectl get restores.br.sealos.io.sealos.io --all-namespaces -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

# 遍历并打印 PVC 的名称
for pvc_name in $pvc_names; do
  # echo "PVC Name: $pvc_name"
  yaml=$(cat yaml/restore.yaml)
  echo "$yaml" | sed "s|{PVC}|$pvc_name|g" | kubectl delete -f -
  
  # 在这里可以添加其他操作，针对每个 PVC 进行处理
  
done
