#!bin/sh

mkdir /backup
mount $MOUNT /backup
cd /backup
restic backup --tag $BACKUPNAME --tag $NAMESPACE --no-cache . & wait $!