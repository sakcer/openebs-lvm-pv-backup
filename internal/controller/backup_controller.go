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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

const (
	Condition  = "BackupCondition"
	BackupPV   = "BackupPV"
	BackupPVC  = "BackupPVC"
	Complete   = "Complete"
	Finalizing = "Finalizing"
)

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

	if backup.Status.Phase == Complete {
		return ctrl.Result{}, nil
	}

	ok, err := r.validController(ctx, backup)
	if err != nil {
		fmt.Println(err)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	fmt.Println("Current Phase", backup.Status.Phase)

	switch backup.Status.Phase {
	case "":
		backup.Status.Node = r.NodeName
		backup.Status.Phase = Condition
	case Condition:
		if err := r.checkPvPvc(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case BackupPV:
		if err := r.syncBackupPv(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case BackupPVC:
		if err := r.syncBackupPvc(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	case Finalizing:
		if err := r.syncFinalizing(ctx, backup); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	if err := r.Status().Update(ctx, backup); err != nil {
		return ctrl.Result{}, err
	}

	if backup.Status.Phase != Complete {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *BackupReconciler) syncFinalizing(ctx context.Context, backup *backupv1.Backup) error {
	fmt.Println("start finalizing")

	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Name,
	}, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		if pod.Labels["backup"] != "" {
			return r.Delete(ctx, pod)
		}
	}
	backup.Status.Phase = Complete
	return nil
}

func (r *BackupReconciler) checkPvPvc(ctx context.Context, backup *backupv1.Backup) error {
	fmt.Println("Condition")
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {
		return err
	}
	fmt.Println(pvc.Name)

	pod, err := r.getPvcPod(ctx, pvc)
	if err != nil {
		return err
	} else if pod != nil {
		backup.Status.Phase = BackupPV
		return nil
	}

	if err := r.helpPod(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Name,
	}, pvc.Name); err != nil {
		return err
	}

	backup.Status.Phase = BackupPV

	return nil
}

func (r *BackupReconciler) validController(ctx context.Context, backup *backupv1.Backup) (bool, error) {
	fmt.Println("valid check")
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

	// fmt.Println("Get PodList")
	// podList := &corev1.PodList{}
	// if err := r.List(ctx, podList, &client.ListOptions{
	// 	Namespace: backup.Namespace,
	// }); err != nil {
	// 	return false, err
	// }
	// for _, pod := range podList.Items {
	// 	fmt.Println(pod.Name)
	// 	for _, volume := range pod.Spec.Volumes {
	// 		if volume.PersistentVolumeClaim == nil {
	// 			continue
	// 		}
	// 		if volume.PersistentVolumeClaim.ClaimName == pvc.Spec.VolumeName {
	// 			backup.Status.Phase = BackupPV
	// 			return true, nil
	// 		}
	// 	}
	// }

	return true, nil
}

func (r *BackupReconciler) syncBackupPvc(ctx context.Context, backup *backupv1.Backup) error {
	fmt.Println("start backup pvc")
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
	backup.Status.Phase = Finalizing
	return nil
}

func (r *BackupReconciler) getPvcPod(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList); err != nil {
		return &corev1.Pod{}, err
	}
	for _, pod := range podList.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			if volume.PersistentVolumeClaim.ClaimName == pvc.Name {
				return &pod, nil
			}
		}
	}
	return nil, nil
}

func (r *BackupReconciler) syncBackupPv(ctx context.Context, backup *backupv1.Backup) error {
	fmt.Println("start backup pv")
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: backup.Namespace,
		Name:      backup.Spec.PersistentVolumeClaim.Name,
	}, pvc); err != nil {
		return err
	}

	pod, err := r.getPvcPod(ctx, pvc)
	if err != nil {

	} else if pod == nil {
		return errors.New("no pod using the pvc")
	}

	// if err := r.backupJob(ctx, types.NamespacedName{
	// 	Namespace: backup.Namespace,
	// 	Name:      backup.Name,
	// }, fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes.io~csi/%s/mount", pod.UID, pvc.Spec.VolumeName)); err != nil {
	// 	return err
	// }

	path := fmt.Sprintf("/data/%s/volumes/kubernetes.io~csi/%s/mount", string(pod.UID), pvc.Spec.VolumeName)
	ok, err := r.BackupOp.Backup(path, backupv1.Tags{
		Namespace:  backup.Namespace,
		BackupName: backup.Name,
	})
	if !ok {
		return err
	}
	fmt.Println(path)

	fmt.Printf("Warning: %s\n", err)
	backup.Status.Phase = BackupPVC
	return nil
}

func (r *BackupReconciler) helpPod(ctx context.Context, req types.NamespacedName, pvc string) error {
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
			Labels: map[string]string{
				"backup": "hello",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "backup",
					Image: "busybox:latest",
					Command: []string{
						"/bin/sh",
						"-c",
						"while true; do echo hello world; sleep 1; done",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "backup",
							MountPath: "/data",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "backup",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc,
						},
					},
				},
			},
		},
	}

	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, pod, func() error {
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (r *BackupReconciler) nodeAffinity(node string) *corev1.Affinity {
	return &corev1.Affinity{
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
	}
}

func (r *BackupReconciler) genEnv(backupName, namespace string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "BACKUPNAME",
			Value: backupName,
		},
		{
			Name:  "NAMESPACE",
			Value: namespace,
		},
		{
			Name:  "AWS_ACCESS_KEY_ID",
			Value: backupv1.AccessKeyID,
		},
		{
			Name:  "AWS_SECRET_ACCESS_KEY",
			Value: backupv1.SecretAccessKey,
		},
		{
			Name:  "RESTIC_REPOSITORY",
			Value: backupv1.Repo,
		},
		{
			Name:  "RESTIC_PASSWORD",
			Value: backupv1.Password,
		},
	}
}

func (r *BackupReconciler) backupJob(ctx context.Context, req types.NamespacedName, path string) error {
	job := &batchv1.Job{
		ObjectMeta: v1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: pointer.Int32(3600),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Affinity:      r.nodeAffinity(r.NodeName),
					Containers: []corev1.Container{
						{
							Name:    "backup",
							Image:   "sakcer/restic:latest",
							Command: []string{"/restic.sh"},
							Env:     r.genEnv(req.Name, req.Namespace),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "backup",
									MountPath: "/bakcup",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "backup",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: path,
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
		return client.IgnoreAlreadyExists(err)
	}
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

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backupv1.Backup{}).
		Watches(
			&batchv1.Job{},
			handler.EnqueueRequestsFromMapFunc(r.triggerReconcileBecauseExternalHasChanged),
			builder.WithPredicates(predicate.Funcs{})).
		Complete(r)
}

func (r *BackupReconciler) triggerReconcileBecauseExternalHasChanged(ctx context.Context, o client.Object) []reconcile.Request {
	namespaced := types.NamespacedName{
		Name:      o.GetName(),
		Namespace: o.GetNamespace(),
	}

	if namespaced.Name != "" && namespaced.Namespace != "" {
		requests := reconcile.Request{NamespacedName: namespaced}
		return []reconcile.Request{requests}
	}
	return []reconcile.Request{}
}
