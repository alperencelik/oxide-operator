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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SnapshotSpec defines the desired state of an Oxide disk snapshot.
// +kubebuilder:validation:XValidation:rule="has(self.project) != has(self.projectRef)",message="exactly one of project or projectRef must be set"
type SnapshotSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Disk is the name or ID of the disk to snapshot.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="disk is immutable"
	Disk string `json:"disk"`
	// Description of the snapshot. Applied at creation.
	// +optional
	Description string `json:"description,omitempty"`
}

// SnapshotStatus defines the observed state of Snapshot.
type SnapshotStatus struct {
	ResourceStatus `json:",inline"`
	// State is the Oxide snapshot state, e.g. creating or ready.
	// +optional
	State string `json:"state,omitempty"`
	// Size of the snapshot.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oxsnap
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.project`
// +kubebuilder:printcolumn:name="Disk",type=string,JSONPath=`.spec.disk`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.status.size`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Snapshot is the Schema for the snapshots API. The object name is the Oxide snapshot name.
type Snapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SnapshotSpec   `json:"spec"`
	Status SnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SnapshotList contains a list of Snapshot.
type SnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Snapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Snapshot{}, &SnapshotList{})
		return nil
	})
}
