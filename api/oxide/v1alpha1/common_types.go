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

package v1alpha1

import (
	"cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ReconcileModeAnnotation controls how the operator reconciles a resource.
	ReconcileModeAnnotation = "oxide.100vms.com/reconcile-mode"
	// ReconcileModeDisable stops all reconciliation for the resource.
	ReconcileModeDisable = "Disable"

	RunStateRunning = "Running"
	RunStateStopped = "Stopped"

	ExternalIPEphemeral = "Ephemeral"
	ExternalIPFloating  = "Floating"
)

// ResourceSpec holds the fields shared by every Oxide-backed resource.
type ResourceSpec struct {
	// ConnectionRef is the name of the cluster-scoped OxideConnection to use.
	ConnectionRef corev1.LocalObjectReference `json:"connectionRef"`
	// DeletionProtection keeps the Oxide resource when the Kubernetes object is deleted.
	// +optional
	DeletionProtection bool `json:"deletionProtection,omitempty"`
}

// ResourceStatus holds the status fields shared by every Oxide-backed resource.
type ResourceStatus struct {
	// ID is the Oxide ID of the resource.
	// +optional
	ID string `json:"id,omitempty"`
	// Project is the Oxide project the resource lives in, resolved from spec.project
	// or spec.projectRef. Empty for resources that are not project scoped.
	// +optional
	Project string `json:"project,omitempty"`
	// Conditions describe the current state of the resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last generation reconciled successfully.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// ProjectRefSpec sets the Oxide project of a resource, by name or through a Project object.
// Specs embedding it validate which of the two is required.
// +kubebuilder:validation:XValidation:rule="has(self.projectRef) == has(oldSelf.projectRef)",message="project is immutable"
type ProjectRefSpec struct {
	// Project is the name or ID of the Oxide project.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="project is immutable"
	// +optional
	Project string `json:"project,omitempty"`
	// ProjectRef references a Project in the same namespace.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="projectRef is immutable"
	// +optional
	ProjectRef *corev1.LocalObjectReference `json:"projectRef,omitempty"`
}

// ProjectName returns the Oxide project name. A Project's object name is its Oxide name.
func (s ProjectRefSpec) ProjectName() string {
	if s.ProjectRef != nil {
		return s.ProjectRef.Name
	}
	return s.Project
}

// OxideNameSpec sets the Oxide name of a project-scoped resource.
// +kubebuilder:validation:XValidation:rule="has(self.name) == has(oldSelf.name)",message="name is immutable"
type OxideNameSpec struct {
	// Name is the Oxide name of the resource. Defaults to the object name; set it when objects
	// in different namespaces would otherwise map to the same Oxide resource.
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	// +optional
	Name string `json:"name,omitempty"`
}

// OxideName returns spec.name, defaulting to the name of obj.
func (s OxideNameSpec) OxideName(obj metav1.Object) string {
	return cmp.Or(s.Name, obj.GetName())
}
