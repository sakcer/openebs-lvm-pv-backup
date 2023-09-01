package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BackupOperator struct {
	Minio       *minio.Client
	ObjectLocks map[string]*sync.Mutex
	Lock        sync.Mutex
}

func (b *BackupOperator) Put(ctx context.Context, bucketName, objectName string, obj client.Object) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	_, err = b.Minio.PutObject(ctx, bucketName, objectName, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (b *BackupOperator) Get(ctx context.Context, bucketName, objectName string, pvc client.Object) error {
	obj, err := b.Minio.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()

	var pvcBytes []byte
	buf := make([]byte, 1024) // Buffer to temporarily store object data
	for {
		n, err := obj.Read(buf)
		pvcBytes = append(pvcBytes, buf[:n]...)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}

	if err := json.Unmarshal(pvcBytes, pvc); err != nil {
		return err
	}
	return nil
}
