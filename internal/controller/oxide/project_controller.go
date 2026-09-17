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

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=projects/finalizers,verbs=update

// Reconcile creates, updates and deletes the Oxide project behind a Project.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	project := &oxidev1alpha1.Project{}
	if err := r.Get(ctx, req.NamespacedName, project); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, project) {
		return ctrl.Result{}, nil
	}
	if !project.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, project)
	}
	if err := r.handleFinalizer(ctx, project); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(project.DeepCopy())
	res, err := setReady(&project.Status.Conditions, r.handleProjectOperations(ctx, project))
	if meta.IsStatusConditionTrue(project.Status.Conditions, typeReady) {
		project.Status.ObservedGeneration = project.Generation
	}
	if perr := r.Status().Patch(ctx, project, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleProjectOperations creates the project if it's missing and keeps its description in sync.
func (r *ProjectReconciler) handleProjectOperations(ctx context.Context, project *oxidev1alpha1.Project) error {
	logger := log.FromContext(ctx)
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, project.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	name := oxide.NameOrId(project.Name)
	cur, err := oc.ProjectView(ctx, oxide.ProjectViewParams{Project: name})
	switch {
	case errors.Is(err, oxide.ErrObjectNotFound):
		logger.Info("Creating Oxide project")
		cur, err = oc.ProjectCreate(ctx, oxide.ProjectCreateParams{
			Body: &oxide.ProjectCreate{Name: oxide.Name(project.Name), Description: project.Spec.Description},
		})
	case err == nil && project.Spec.Description != "" && cur.Description != project.Spec.Description:
		logger.Info("Updating Oxide project")
		cur, err = oc.ProjectUpdate(ctx, oxide.ProjectUpdateParams{
			Project: name, Body: &oxide.ProjectUpdate{Description: project.Spec.Description},
		})
	}
	if err != nil {
		return err
	}
	project.Status.ID = cur.Id
	return nil
}

func (r *ProjectReconciler) handleFinalizer(ctx context.Context, project *oxidev1alpha1.Project) error {
	if controllerutil.AddFinalizer(project, finalizerName) {
		return r.Update(ctx, project)
	}
	return nil
}

// handleDelete deletes the Oxide project, unless protected, and removes the finalizer.
func (r *ProjectReconciler) handleDelete(ctx context.Context, project *oxidev1alpha1.Project) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(project, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !project.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, project.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		name := oxide.NameOrId(project.Name)
		// Oxide creates a "default" VPC with every project and won't delete a project that still has VPCs.
		err = deleteVpc(ctx, oc, name, "default")
		if err == nil || errors.Is(err, oxide.ErrObjectNotFound) {
			err = oc.ProjectDelete(ctx, oxide.ProjectDeleteParams{Project: name})
		}
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide project")
	}
	controllerutil.RemoveFinalizer(project, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, project))
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Project{}, builder.WithPredicates(changed)).
		Named("oxide-project").
		Complete(r)
}
