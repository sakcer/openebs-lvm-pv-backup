#!bin/sh

mkdir /restore
mount $MOUNT /restore
cd /restore
restic restore $SNAPSHOT --target . & wait $!