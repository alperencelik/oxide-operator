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
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

// SnapshotReconciler reconciles a Snapshot object
type SnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=snapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=snapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=snapshots/finalizers,verbs=update

// Reconcile creates and deletes the Oxide snapshot behind a Snapshot.
func (r *SnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	snapshot := &oxidev1alpha1.Snapshot{}
	if err := r.Get(ctx, req.NamespacedName, snapshot); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, snapshot) {
		return ctrl.Result{}, nil
	}
	log.FromContext(ctx).Info("Reconciling Snapshot")
	if !snapshot.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, snapshot)
	}
	if err := r.handleFinalizer(ctx, snapshot); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(snapshot.DeepCopy())
	res, err := setReady(&snapshot.Status.Conditions, r.handleSnapshotOperations(ctx, snapshot))
	if meta.IsStatusConditionTrue(snapshot.Status.Conditions, typeReady) {
		snapshot.Status.ObservedGeneration = snapshot.Generation
	}
	if perr := r.Status().Patch(ctx, snapshot, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleSnapshotOperations creates the snapshot if it's missing and records its state.
func (r *SnapshotReconciler) handleSnapshotOperations(ctx context.Context, snapshot *oxidev1alpha1.Snapshot) error {
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, snapshot.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	project, name := oxide.NameOrId(snapshot.Spec.ProjectName()), oxide.NameOrId(snapshot.Spec.OxideName(snapshot))
	cur, err := oc.SnapshotView(ctx, oxide.SnapshotViewParams{Project: project, Snapshot: name})
	if errors.Is(err, oxide.ErrObjectNotFound) {
		log.FromContext(ctx).Info("Creating Oxide snapshot")
		cur, err = oc.SnapshotCreate(ctx, oxide.SnapshotCreateParams{Project: project, Body: &oxide.SnapshotCreate{
			Name: oxide.Name(name), Description: snapshot.Spec.Description, Disk: oxide.NameOrId(snapshot.Spec.Disk),
		}})
	}
	if err != nil {
		return err
	}
	snapshot.Status.ID, snapshot.Status.State = cur.Id, string(cur.State)
	snapshot.Status.Project = snapshot.Spec.ProjectName()
	snapshot.Status.Size = resource.NewQuantity(int64(cur.Size), resource.BinarySI)
	switch cur.State {
	case oxide.SnapshotStateFaulted:
		return errors.New("snapshot is faulted")
	case oxide.SnapshotStateCreating:
		return progressing("snapshot is creating")
	}
	return nil
}

func (r *SnapshotReconciler) handleFinalizer(ctx context.Context, snapshot *oxidev1alpha1.Snapshot) error {
	if controllerutil.AddFinalizer(snapshot, finalizerName) {
		return r.Update(ctx, snapshot)
	}
	return nil
}

// handleDelete deletes the Oxide snapshot, unless protected, and removes the finalizer.
func (r *SnapshotReconciler) handleDelete(ctx context.Context, snapshot *oxidev1alpha1.Snapshot) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(snapshot, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !snapshot.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, snapshot.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = oc.SnapshotDelete(ctx, oxide.SnapshotDeleteParams{
			Project: oxide.NameOrId(snapshot.Spec.ProjectName()), Snapshot: oxide.NameOrId(snapshot.Spec.OxideName(snapshot)),
		})
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide snapshot")
	}
	controllerutil.RemoveFinalizer(snapshot, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, snapshot))
}

// SetupWithManager sets up the controller with the Manager.
func (r *SnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Snapshot{}, builder.WithPredicates(changed)).
		Named("oxide-snapshot").
		Complete(r)
}
