/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	// "github.com/sakcer/kubectl-neat/pkg/defaults"

	backupv1 "restic/api/v1"
	backupfn "restic/backup"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	BackupOp *backupfn.BackupOperator
	NodeName string
}

//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=backups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=backups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=backups/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Backup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	fmt.Println("reconcile")

	backup := &backupv1.Backup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ok, err := r.validController(ctx, backup)
	if err != nil {
		fmt.Println(err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	switch backup.Status.Phase {
	case "":
		backup.Status.Node = r.NodeName
		backup.Status.Phase = "BackupPv"
	case "BackupPv":
		if err := r.syncBackupPv(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case "BackupPvc":
		if err := r.syncBackupPvc(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	if err := r.Status().Update(ctx, backup); err != nil {
		return ctrl.Result{}, err
	}

	if backup.Status.Phase != "Completed" {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *BackupReconciler) validController(ctx context.Context, backup *backupv1.Backup) (bool, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      backup.Spec.PersistentVolumeClaim.Name,
		Namespace: backup.Namespace,
	}, pvc); err != nil {
		return false, err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return false, errors.New("pvc not bounded")
	}

	if r.NodeName != pvc.Annotations["volume.kubernetes.io/selected-node"] {
		return false, nil
	}
	return true, nil
}

func (r *BackupReconciler) syncBackupPvc(ctx context.Context, backup *backupv1.Backup) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {
		return err
	}

	objectName := fmt.Sprintf("%s/%s/%s", backup.Namespace, backup.Name, pvc.Name)
	if err := r.BackupOp.Put(ctx, "tmp", objectName, r.pvcNeat(pvc)); err != nil {
		return err
	}
	backup.Status.Phase = "Completed"
	return nil
}

func (r *BackupReconciler) syncBackupPv(ctx context.Context, backup *backupv1.Backup) error {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {
		return err
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return errors.New("pvc not bounded")
	}

	// lock := r.getObjectLock(pvc.Spec.VolumeName)
	// lock.Lock()
	// r.BackupOp.Lock.Lock()
	ok, err := r.BackupOp.Backup(pvc.Spec.VolumeName, string(backup.UID), backupv1.Tags{
		Namespace:  backup.Namespace,
		BackupName: backup.Name})
	// defer r.BackupOp.Lock.Unlock()
	// defer lock.Unlock()

	if !ok {
		return err
	}

	fmt.Printf("Warning: %s\n", err)
	backup.Status.Phase = "BackupPvc"
	return nil
}

func (r *BackupReconciler) pvcNeat(pvc *corev1.PersistentVolumeClaim) *corev1.PersistentVolumeClaim {
	fmt.Println("start neat")
	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: v1.ObjectMeta{
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
			Labels:    pvc.Labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      pvc.Spec.AccessModes,
			Resources:        pvc.Spec.Resources,
			StorageClassName: pvc.Spec.StorageClassName,
		},
	}

	return result
}

// 获取对象锁
func (b *BackupReconciler) getObjectLock(object string) *sync.Mutex {
	// 获取对象对应的锁，如果不存在则创建新的锁
	b.BackupOp.Lock.Lock()
	if lock, ok := b.BackupOp.ObjectLocks[object]; ok {
		b.BackupOp.Lock.Unlock()
		return lock
	}
	lock := &sync.Mutex{}
	b.BackupOp.ObjectLocks[object] = lock
	b.BackupOp.Lock.Unlock()
	return lock
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backupv1.Backup{}).
		Complete(r)
}
