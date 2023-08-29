#!/bin/bash

# 定义YAML文件名
files="backup.yaml"

# 使用for循环逐个应用YAML文件
for file in $files; do
    kubectl apply -f "$file"
done
