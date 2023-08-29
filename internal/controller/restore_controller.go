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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	backupv1 "restic/api/v1"
	backupfn "restic/backup"
)

// RestoreReconciler reconciles a Restore object
type RestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	BackupOp backupfn.BackupOperator
	NodeName string
}

//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=restores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=br.sealos.io.sealos.io,resources=restores/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Restore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	fmt.Println("reconcile")

	restore := &backupv1.Restore{}
	if err := r.Get(ctx, req.NamespacedName, restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ok, err := r.validController(ctx, restore)
	if err != nil {
		fmt.Println(err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	switch restore.Status.Phase {
	case "":
		restore.Status.Phase = "Starting"
	case "Starting":
		if err := r.syncStarting(ctx, restore); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
		}
	case "RestorePvc":
		if err := r.syncRestorePvc(ctx, restore); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
		}
	case "RestorePv":
		if err := r.syncRestorePv(ctx, restore); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
		}
	}

	if err := r.Status().Update(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}

	if restore.Status.Phase != "Completed" {
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	return ctrl.Result{}, nil
}

func (r *RestoreReconciler) validController(ctx context.Context, restore *backupv1.Restore) (bool, error) {
	backup := &backupv1.Backup{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      restore.Spec.BackupName,
		Namespace: restore.Namespace,
	}, backup); err != nil {
		return false, err
	}

	if r.NodeName != backup.Status.Node {
		return false, nil
	}
	return true, nil
}

func (r *RestoreReconciler) syncStarting(ctx context.Context, restore *backupv1.Restore) error {
	backup := &backupv1.Backup{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Spec.BackupName,
	}, backup); err != nil {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		restore.Status.Phase = "RestorePvc"
	} else {
		restore.Status.Phase = "RestorePv"
	}
	return nil
}

func (r *RestoreReconciler) syncRestorePvc(ctx context.Context, restore *backupv1.Restore) error {
	fmt.Println("start restore pvc")
	backup := &backupv1.Backup{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Spec.BackupName,
	}, backup); err != nil {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{}
	objectName := fmt.Sprintf("%s/%s/%s", backup.Namespace, backup.Name, backup.Spec.PersistentVolumeClaim.Name)
	if err := r.BackupOp.Get(ctx, "tmp", objectName, pvc); err != nil {
		return err
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		return nil
	}); err != nil {
		return err
	}

	if err := r.syncJob(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Name,
	}, r.NodeName, pvc.Name); err != nil {
		return err
	}

	if pvc.Status.Phase == corev1.ClaimBound {
		restore.Status.Phase = "RestorePv"
	}

	return nil
}

func (r *RestoreReconciler) syncRestorePv(ctx context.Context, restore *backupv1.Restore) error {
	fmt.Println("start restore pv")
	backup := &backupv1.Backup{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Spec.BackupName,
	}, backup); err != nil {
		return err
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {

	}

	if err := r.BackupOp.Restore(pvc.Spec.VolumeName, backupv1.Tags{
		Namespace:  restore.Namespace,
		BackupName: restore.Spec.BackupName,
	}); err != nil {
		return err
	}

	restore.Status.Phase = "Completed"

	return nil
}

func (r *RestoreReconciler) syncJob(ctx context.Context, req types.NamespacedName, node, pvc string) error {
	job := &batchv1.Job{
		ObjectMeta: v1.ObjectMeta{
			Namespace:    req.Namespace,
			GenerateName: req.Name,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: pointer.Int32(5),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "openebs.io/nodename",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{node},
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "restore",
							Image: "busybox:latest",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "restore",
									MountPath: "/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "restore",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvc,
								},
							},
						},
					},
				},
			},
			BackoffLimit: pointer.Int32(1),
		},
	}
	if err := r.Create(ctx, job); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backupv1.Restore{}).
		Complete(r)
}
