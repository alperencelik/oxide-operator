/*
Copyright 2026.

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

package oxide

import (
	"context"
	"errors"

	"github.com/oxidecomputer/oxide.go/oxide"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

// DiskReconciler reconciles a Disk object
type DiskReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=disks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=disks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=disks/finalizers,verbs=update

// Reconcile creates and deletes the Oxide disk behind a Disk.
func (r *DiskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	disk := &oxidev1alpha1.Disk{}
	if err := r.Get(ctx, req.NamespacedName, disk); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, disk) {
		return ctrl.Result{}, nil
	}
	if !disk.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, disk)
	}
	if err := r.handleFinalizer(ctx, disk); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(disk.DeepCopy())
	res, err := setReady(&disk.Status.Conditions, r.handleDiskOperations(ctx, disk))
	if meta.IsStatusConditionTrue(disk.Status.Conditions, typeReady) {
		disk.Status.ObservedGeneration = disk.Generation
	}
	if perr := r.Status().Patch(ctx, disk, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleDiskOperations creates the disk if it's missing and records its state.
func (r *DiskReconciler) handleDiskOperations(ctx context.Context, disk *oxidev1alpha1.Disk) error {
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, disk.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	project, name := oxide.NameOrId(disk.Spec.ProjectName()), oxide.NameOrId(disk.Spec.OxideName(disk))
	cur, err := oc.DiskView(ctx, oxide.DiskViewParams{Project: project, Disk: name})
	if errors.Is(err, oxide.ErrObjectNotFound) {
		log.FromContext(ctx).Info("Creating Oxide disk")
		src, serr := diskSource(ctx, oc, disk.Spec.ProjectName(), disk.Spec.Image, disk.Spec.Snapshot, disk.Spec.BlockSize)
		if serr != nil {
			return serr
		}
		cur, err = oc.DiskCreate(ctx, oxide.DiskCreateParams{Project: project, Body: &oxide.DiskCreate{
			Name:        oxide.Name(name),
			Description: disk.Spec.Description,
			Size:        oxide.ByteCount(disk.Spec.Size.Value()),
			DiskBackend: oxide.DiskBackend{Value: &oxide.DiskBackendDistributed{DiskSource: src}},
		}})
	}
	if err != nil {
		return err
	}
	disk.Status.ID, disk.Status.State = cur.Id, string(cur.State.State())
	switch cur.State.State() {
	case oxide.DiskStateStateFaulted:
		return errors.New("disk is faulted")
	case oxide.DiskStateStateCreating:
		return progressing("disk is creating")
	}
	return nil
}

func (r *DiskReconciler) handleFinalizer(ctx context.Context, disk *oxidev1alpha1.Disk) error {
	if controllerutil.AddFinalizer(disk, finalizerName) {
		return r.Update(ctx, disk)
	}
	return nil
}

// handleDelete deletes the Oxide disk, unless protected, and removes the finalizer.
func (r *DiskReconciler) handleDelete(ctx context.Context, disk *oxidev1alpha1.Disk) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(disk, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !disk.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, disk.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = oc.DiskDelete(ctx, oxide.DiskDeleteParams{Project: oxide.NameOrId(disk.Spec.ProjectName()), Disk: oxide.NameOrId(disk.Spec.OxideName(disk))})
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide disk")
	}
	controllerutil.RemoveFinalizer(disk, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, disk))
}

// diskSource resolves an image or snapshot to a disk source, defaulting to a blank disk.
func diskSource(ctx context.Context, oc *oxide.Client, project, image, snapshot string, blockSize int) (oxide.DiskSource, error) {
	switch {
	case image != "":
		img, err := viewImage(ctx, oc, project, image)
		if err != nil {
			return oxide.DiskSource{}, err
		}
		return oxide.DiskSource{Value: &oxide.DiskSourceImage{ImageId: img.Id}}, nil
	case snapshot != "":
		snap, err := oc.SnapshotView(ctx, oxide.SnapshotViewParams{Project: oxide.NameOrId(project), Snapshot: oxide.NameOrId(snapshot)})
		if err != nil {
			return oxide.DiskSource{}, err
		}
		return oxide.DiskSource{Value: &oxide.DiskSourceSnapshot{SnapshotId: snap.Id}}, nil
	}
	return oxide.DiskSource{Value: &oxide.DiskSourceBlank{BlockSize: oxide.BlockSize(blockSize)}}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DiskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Disk{}, builder.WithPredicates(changed)).
		Named("oxide-disk").
		Complete(r)
}
