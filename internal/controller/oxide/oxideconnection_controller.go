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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/oxidecomputer/oxide.go/oxide"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

// OxideConnectionReconciler reconciles a OxideConnection object
type OxideConnectionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=oxideconnections,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=oxideconnections/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=oxideconnections/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile checks that the connection's token works and records who it authenticates as.
func (r *OxideConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	conn := &oxidev1alpha1.OxideConnection{}
	if err := r.Get(ctx, req.NamespacedName, conn); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, conn) {
		return ctrl.Result{}, nil
	}
	log.FromContext(ctx).Info("Reconciling OxideConnection")
	if !conn.DeletionTimestamp.IsZero() {
		res, err := r.handleDelete(ctx, conn)
		return res, warn(r.Recorder, conn, "DeleteFailed", err)
	}
	if controllerutil.AddFinalizer(conn, finalizerName) {
		if err := r.Update(ctx, conn); err != nil {
			return ctrl.Result{}, err
		}
	}

	patch := client.MergeFrom(conn.DeepCopy())
	// Recording the Secret's resourceVersion makes controller rebuild its client after a token rotation.
	ref := conn.Spec.TokenSecretRef
	secret := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
	var oc *oxide.Client
	err := r.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, secret)
	if err == nil {
		conn.Status.ObservedSecretResourceVersion = secret.ResourceVersion
		oc, err = oxideclient.ClientFor(ctx, r.Client, conn)
	}
	if err == nil {
		me, verr := oc.CurrentUserView(ctx)
		if err = verr; err == nil {
			conn.Status.Silo, conn.Status.User = string(me.SiloName), me.DisplayName
		}
	}
	res, err := setReady(r.Recorder, conn, &conn.Status.Conditions, err)
	if perr := r.Status().Patch(ctx, conn, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleDelete keeps the connection until no resource uses it, so their own deletions
// can still reach Oxide (e.g. when everything is deleted at once).
func (r *OxideConnectionReconciler) handleDelete(ctx context.Context, conn *oxidev1alpha1.OxideConnection) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(conn, finalizerName) {
		return ctrl.Result{}, nil
	}
	inUse, err := r.inUse(ctx, conn.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if inUse {
		return ctrl.Result{RequeueAfter: progressPeriod}, nil
	}
	controllerutil.RemoveFinalizer(conn, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, conn))
}

// inUse reports whether any Oxide resource still references the connection.
// InstanceSets are skipped: the Instances they create carry the reference.
func (r *OxideConnectionReconciler) inUse(ctx context.Context, name string) (bool, error) {
	projects, vpcs, subnets := &oxidev1alpha1.ProjectList{}, &oxidev1alpha1.VpcList{}, &oxidev1alpha1.VpcSubnetList{}
	disks, snapshots, instances := &oxidev1alpha1.DiskList{}, &oxidev1alpha1.SnapshotList{}, &oxidev1alpha1.InstanceList{}
	images := &oxidev1alpha1.ImageList{}
	for _, list := range []client.ObjectList{projects, vpcs, subnets, disks, snapshots, instances, images} {
		if err := r.List(ctx, list); err != nil {
			return false, err
		}
	}
	uses := func(spec oxidev1alpha1.ResourceSpec) bool { return spec.ConnectionRef.Name == name }
	for _, p := range projects.Items {
		if uses(p.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, v := range vpcs.Items {
		if uses(v.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, s := range subnets.Items {
		if uses(s.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, d := range disks.Items {
		if uses(d.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, s := range snapshots.Items {
		if uses(s.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, i := range instances.Items {
		if uses(i.Spec.ResourceSpec) {
			return true, nil
		}
	}
	for _, i := range images.Items {
		if uses(i.Spec.ResourceSpec) {
			return true, nil
		}
	}
	return false, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OxideConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.OxideConnection{}, builder.WithPredicates(changed)).
		// Metadata only, so Secret data is never cached; token rotations reconcile the connections using them.
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.connectionsForSecret)).
		Named("oxide-oxideconnection").
		Complete(r)
}

// connectionsForSecret maps a Secret to the OxideConnections whose token it holds.
func (r *OxideConnectionReconciler) connectionsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	conns := &oxidev1alpha1.OxideConnectionList{}
	if err := r.List(ctx, conns); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list OxideConnections for Secret", "secret", client.ObjectKeyFromObject(secret))
		return nil
	}
	var reqs []reconcile.Request
	for _, c := range conns.Items {
		if ref := c.Spec.TokenSecretRef; ref.Namespace == secret.GetNamespace() && ref.Name == secret.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKey{Name: c.Name}})
		}
	}
	return reqs
}
