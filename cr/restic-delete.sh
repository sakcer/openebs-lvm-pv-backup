#!/bin/bash

ids=$(restic snapshots --json --quiet | jq '.[].short_id')

for id in $ids; do
    nid=$(echo $id | sed 's/"//g')
    echo $(restic forget "${nid}")
done
