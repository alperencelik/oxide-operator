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
	"fmt"
	"maps"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
)

const instanceSetLabel = "oxide.100vms.com/instanceset"

// InstanceSetReconciler reconciles a InstanceSet object
type InstanceSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instancesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instancesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instancesets/finalizers,verbs=update

// Reconcile keeps Instances <set>-0 ... <set>-N in line with the set's template.
// Instances are owned by the set and garbage collected with it.
func (r *InstanceSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	set := &oxidev1alpha1.InstanceSet{}
	if err := r.Get(ctx, req.NamespacedName, set); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, set) || !set.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	patch := client.MergeFrom(set.DeepCopy())
	res, err := setReady(&set.Status.Conditions, r.sync(ctx, set))
	if meta.IsStatusConditionTrue(set.Status.Conditions, typeReady) {
		set.Status.ObservedGeneration = set.Generation
	}
	if perr := r.Status().Patch(ctx, set, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// ponytail: template changes roll out to all Instances at once; add a rolling update if resizes must not overlap.
func (r *InstanceSetReconciler) sync(ctx context.Context, set *oxidev1alpha1.InstanceSet) error {
	var ready int32
	for i := range set.Spec.Replicas {
		inst := &oxidev1alpha1.Instance{ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", set.Name, i), Namespace: set.Namespace,
		}}
		_, err := controllerutil.CreateOrPatch(ctx, r.Client, inst, func() error {
			if inst.Labels == nil {
				inst.Labels = map[string]string{}
			}
			maps.Copy(inst.Labels, set.Spec.Template.Labels)
			inst.Labels[instanceSetLabel] = set.Name
			inst.Spec = set.Spec.Template.Spec
			if inst.Spec.Name != "" {
				inst.Spec.Name = fmt.Sprintf("%s-%d", inst.Spec.Name, i)
			}
			return controllerutil.SetControllerReference(set, inst, r.Scheme)
		})
		if err != nil {
			return err
		}
		if meta.IsStatusConditionTrue(inst.Status.Conditions, typeReady) {
			ready++
		}
	}

	children := &oxidev1alpha1.InstanceList{}
	if err := r.List(ctx, children, client.InNamespace(set.Namespace), client.MatchingLabels{instanceSetLabel: set.Name}); err != nil {
		return err
	}
	for i := range children.Items {
		idx, err := strconv.Atoi(strings.TrimPrefix(children.Items[i].Name, set.Name+"-"))
		if err == nil && idx < int(set.Spec.Replicas) {
			continue
		}
		if err := r.Delete(ctx, &children.Items[i]); client.IgnoreNotFound(err) != nil {
			return err
		}
	}

	set.Status.Replicas, set.Status.ReadyReplicas = set.Spec.Replicas, ready
	if ready < set.Spec.Replicas {
		return progressing(fmt.Sprintf("%d/%d instances ready", ready, set.Spec.Replicas))
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.InstanceSet{}, builder.WithPredicates(changed)).
		Owns(&oxidev1alpha1.Instance{}).
		Named("oxide-instanceset").
		Complete(r)
}
