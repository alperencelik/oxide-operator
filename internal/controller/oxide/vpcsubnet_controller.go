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
	"fmt"

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

// VpcSubnetReconciler reconciles a VpcSubnet object
type VpcSubnetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcsubnets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcsubnets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcsubnets/finalizers,verbs=update
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcs,verbs=get;list;watch

// Reconcile creates, updates and deletes the Oxide subnet behind a VpcSubnet.
func (r *VpcSubnetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	subnet := &oxidev1alpha1.VpcSubnet{}
	if err := r.Get(ctx, req.NamespacedName, subnet); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, subnet) {
		return ctrl.Result{}, nil
	}
	if !subnet.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, subnet)
	}
	if err := r.handleFinalizer(ctx, subnet); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(subnet.DeepCopy())
	res, err := setReady(&subnet.Status.Conditions, r.handleVpcSubnetOperations(ctx, subnet))
	if meta.IsStatusConditionTrue(subnet.Status.Conditions, typeReady) {
		subnet.Status.ObservedGeneration = subnet.Generation
	}
	if perr := r.Status().Patch(ctx, subnet, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleVpcSubnetOperations creates the subnet if it's missing and keeps its description in sync.
func (r *VpcSubnetReconciler) handleVpcSubnetOperations(ctx context.Context, subnet *oxidev1alpha1.VpcSubnet) error {
	logger := log.FromContext(ctx)
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, subnet.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	project, vpc, err := r.subnetVpc(ctx, subnet, true)
	if err != nil {
		return err
	}
	name := oxide.NameOrId(subnet.Spec.OxideName(subnet))

	cur, err := oc.VpcSubnetView(ctx, oxide.VpcSubnetViewParams{Project: project, Vpc: vpc, Subnet: name})
	switch {
	case errors.Is(err, oxide.ErrObjectNotFound):
		logger.Info("Creating Oxide subnet")
		cur, err = oc.VpcSubnetCreate(ctx, oxide.VpcSubnetCreateParams{Project: project, Vpc: vpc, Body: &oxide.VpcSubnetCreate{
			Name:        oxide.Name(name),
			Description: subnet.Spec.Description,
			Ipv4Block:   oxide.Ipv4Net(subnet.Spec.IPv4Block),
			Ipv6Block:   oxide.Ipv6Net(subnet.Spec.IPv6Block),
		}})
	case err == nil && subnet.Spec.Description != "" && cur.Description != subnet.Spec.Description:
		logger.Info("Updating Oxide subnet")
		cur, err = oc.VpcSubnetUpdate(ctx, oxide.VpcSubnetUpdateParams{
			Project: project, Vpc: vpc, Subnet: name, Body: &oxide.VpcSubnetUpdate{Description: subnet.Spec.Description},
		})
	}
	if err != nil {
		return err
	}
	subnet.Status.ID = cur.Id
	return nil
}

func (r *VpcSubnetReconciler) handleFinalizer(ctx context.Context, subnet *oxidev1alpha1.VpcSubnet) error {
	if controllerutil.AddFinalizer(subnet, finalizerName) {
		return r.Update(ctx, subnet)
	}
	return nil
}

// handleDelete deletes the Oxide subnet, unless protected, and removes the finalizer.
func (r *VpcSubnetReconciler) handleDelete(ctx context.Context, subnet *oxidev1alpha1.VpcSubnet) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(subnet, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !subnet.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, subnet.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Delete by ID when known, so a deleted Vpc object doesn't block deleting its subnets.
		params := oxide.VpcSubnetDeleteParams{Subnet: oxide.NameOrId(subnet.Status.ID)}
		if subnet.Status.ID == "" {
			params.Subnet = oxide.NameOrId(subnet.Spec.OxideName(subnet))
			if params.Project, params.Vpc, err = r.subnetVpc(ctx, subnet, false); err != nil {
				return ctrl.Result{}, err
			}
		}
		err = oc.VpcSubnetDelete(ctx, params)
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide subnet")
	}
	controllerutil.RemoveFinalizer(subnet, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, subnet))
}

// subnetVpc returns the Oxide project and VPC of a subnet, from spec.vpc or the Vpc that spec.vpcRef
// points to. With waitReady, a referenced Vpc that isn't Ready yet is reported as progressing.
func (r *VpcSubnetReconciler) subnetVpc(
	ctx context.Context, subnet *oxidev1alpha1.VpcSubnet, waitReady bool,
) (project, vpc oxide.NameOrId, err error) {
	if subnet.Spec.VpcRef == nil {
		return oxide.NameOrId(subnet.Spec.ProjectName()), oxide.NameOrId(subnet.Spec.Vpc), nil
	}
	ref := &oxidev1alpha1.Vpc{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: subnet.Namespace, Name: subnet.Spec.VpcRef.Name}, ref); err != nil {
		return "", "", fmt.Errorf("getting Vpc %q: %w", subnet.Spec.VpcRef.Name, err)
	}
	if waitReady && !meta.IsStatusConditionTrue(ref.Status.Conditions, typeReady) {
		return "", "", progressing(fmt.Sprintf("waiting for Vpc %q to be ready", ref.Name))
	}
	return oxide.NameOrId(ref.Spec.ProjectName()), oxide.NameOrId(ref.Spec.OxideName(ref)), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VpcSubnetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.VpcSubnet{}, builder.WithPredicates(changed)).
		Named("oxide-vpcsubnet").
		Complete(r)
}
