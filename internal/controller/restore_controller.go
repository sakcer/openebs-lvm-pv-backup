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
	BackupOp *backupfn.BackupOperator
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

	if restore.Status.Phase == Complete {
		return ctrl.Result{}, nil
	}

	ok, err := r.validController(ctx, restore)
	if err != nil {
		fmt.Println(err)
		return ctrl.Result{}, nil
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
	case Finalizing:
		if err := r.syncFinalizing(ctx, restore); err != nil {
			fmt.Println(err)
			return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
		}
	}

	if err := r.Status().Update(ctx, restore); err != nil {
		return ctrl.Result{}, err
	}

	if restore.Status.Phase != Complete {
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	return ctrl.Result{}, nil
}

func (r *RestoreReconciler) syncFinalizing(ctx context.Context, restore *backupv1.Restore) error {
	fmt.Println("start finalizing")

	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Name,
	}, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		if pod.Labels["restore"] != "" {
			return r.Delete(ctx, pod)
		}
	}
	restore.Status.Phase = Complete
	return nil
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
		pod, err := r.getPvcPod(ctx, pvc)
		if err != nil {
			return err
		} else if pod == nil {
			restore.Status.Phase = "RestorePvc"
		} else {
			restore.Status.Phase = "RestorePv"
		}
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

	if err := r.helpPod(ctx, types.NamespacedName{
		Namespace: restore.Namespace,
		Name:      restore.Name,
	}, pvc.Name); err != nil {
		return err
	}

	if pvc.Status.Phase == corev1.ClaimBound {
		pod, err := r.getPvcPod(ctx, pvc)
		if err != nil {
			return err
		} else if pod == nil {
			return nil
		} else {
			restore.Status.Phase = "RestorePv"
		}
	}

	return nil
}

func (r *RestoreReconciler) helpPod(ctx context.Context, req types.NamespacedName, pvc string) error {
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
			Labels: map[string]string{
				"restore": "hello",
			},
		},
		Spec: corev1.PodSpec{
			Affinity: r.nodeAffinity(r.NodeName),
			Containers: []corev1.Container{
				{
					Name:  "restore",
					Image: "busybox:latest",
					Command: []string{
						"/bin/sh",
						"-c",
						"while true; do echo hello world; sleep 1; done",
					},
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
	}

	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, pod, func() error {
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (r *RestoreReconciler) getPvcPod(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (*corev1.Pod, error) {
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

	pod, err := r.getPvcPod(ctx, pvc)
	if err != nil {
		return err
	} else if pod == nil {
		return errors.New("no pod using the pvc")
	}

	path := fmt.Sprintf("/data/%s/volumes/kubernetes.io~csi/%s/mount", string(pod.UID), pvc.Spec.VolumeName)
	ok, err := r.BackupOp.Restore(path, backupv1.Tags{
		Namespace:  backup.Namespace,
		BackupName: backup.Name,
	})
	if !ok {
		return err
	}
	fmt.Println(path)

	// snap, err := r.BackupOp.ResticSnapshots(backupv1.Tags{
	// 	Namespace:  restore.Namespace,
	// 	BackupName: restore.Spec.BackupName,
	// })
	// if err != nil {
	// 	return err
	// }

	// if err := r.restoreJob(ctx, types.NamespacedName{
	// 	Namespace: restore.Namespace,
	// 	Name:      restore.Name,
	// }, fmt.Sprintf("/dev/lvmvg/%s", backup.Spec.PersistentVolumeClaim.Name), snap[0].ShortID); err != nil {
	// 	return err
	// }

	// lock := r.getObjectLock(pvc.Spec.VolumeName)
	// r.BackupOp.Lock.Lock()
	// ok, err := r.BackupOp.Restore(pvc.Spec.VolumeName, string(restore.UID), backupv1.Tags{
	// 	Namespace:  restore.Namespace,
	// 	BackupName: restore.Spec.BackupName,
	// })
	// defer r.BackupOp.Lock.Unlock()
	// defer lock.Unlock()
	// if !ok {
	// 	return err
	// }
	// fmt.Printf("Warning: %s\n", err)

	restore.Status.Phase = Finalizing

	return nil
}

func (r *RestoreReconciler) nodeAffinity(node string) *corev1.Affinity {
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

func (r *RestoreReconciler) genEnv(snapshot, mount string) []corev1.EnvVar {
	return []corev1.EnvVar{
		corev1.EnvVar{
			Name:  "SNAPSHOT",
			Value: snapshot,
		},
		corev1.EnvVar{
			Name:  "MOUNT",
			Value: mount,
		},
		corev1.EnvVar{
			Name:  "AWS_ACCESS_KEY_ID",
			Value: backupv1.AccessKeyID,
		},
		corev1.EnvVar{
			Name:  "AWS_SECRET_ACCESS_KEY",
			Value: backupv1.SecretAccessKey,
		},
		corev1.EnvVar{
			Name:  "RESTIC_REPOSITORY",
			Value: backupv1.Repo,
		},
		corev1.EnvVar{
			Name:  "RESTIC_PASSWORD",
			Value: backupv1.Password,
		},
	}
}

func (r *RestoreReconciler) restoreJob(ctx context.Context, req types.NamespacedName, path, snap string) error {

	job := &batchv1.Job{
		ObjectMeta: v1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: pointer.Int32(20),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Affinity:      r.nodeAffinity(r.NodeName),
					Containers: []corev1.Container{
						{
							Name:    "backup",
							Image:   "sakcer/restore:latest",
							Command: []string{"/restic.sh"},
							Env:     r.genEnv(snap, path),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "restore",
									MountPath: "/dev",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.Bool(true),
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "restore",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
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

func (r *RestoreReconciler) syncJob(ctx context.Context, req types.NamespacedName, node, pvc string) error {
	job := &batchv1.Job{
		ObjectMeta: v1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
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
		return client.IgnoreAlreadyExists(err)
	}
	return nil
}

// 获取对象锁
func (b *RestoreReconciler) getObjectLock(object string) *sync.Mutex {
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
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backupv1.Restore{}).
		Complete(r)
}
