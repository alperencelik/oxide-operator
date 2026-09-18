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

// DiskSpec defines the desired state of an Oxide disk.
// +kubebuilder:validation:XValidation:rule="!(has(self.image) && has(self.snapshot))",message="image and snapshot are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="has(self.project) != has(self.projectRef)",message="exactly one of project or projectRef must be set"
type DiskSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Description of the disk. Applied at creation.
	// +optional
	Description string `json:"description,omitempty"`
	// Size of the disk, e.g. 20Gi.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="size is immutable"
	Size resource.Quantity `json:"size"`
	// Image is the name or ID of an image to create the disk from.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="image is immutable"
	// +optional
	Image string `json:"image,omitempty"`
	// Snapshot is the name or ID of a snapshot to create the disk from.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="snapshot is immutable"
	// +optional
	Snapshot string `json:"snapshot,omitempty"`
	// BlockSize of a blank disk in bytes.
	// +kubebuilder:validation:Enum=512;2048;4096
	// +kubebuilder:default=2048
	// +optional
	BlockSize int `json:"blockSize,omitempty"`
}

// DiskStatus defines the observed state of Disk.
type DiskStatus struct {
	ResourceStatus `json:",inline"`
	// State is the Oxide disk state, e.g. detached or attached.
	// +optional
	State string `json:"state,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oxd
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.status.project`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.spec.size`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Disk is the Schema for the disks API. The object name is the Oxide disk name.
type Disk struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DiskSpec   `json:"spec"`
	Status DiskStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DiskList contains a list of Disk.
type DiskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Disk `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Disk{}, &DiskList{})
		return nil
	})
}
