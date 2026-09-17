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

// ImageSpec defines the desired state of an Oxide image.
// +kubebuilder:validation:XValidation:rule="has(self.url) != has(self.snapshot)",message="exactly one of url or snapshot is required"
// +kubebuilder:validation:XValidation:rule="has(self.project) != has(self.projectRef)",message="exactly one of project or projectRef must be set"
type ImageSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Description of the image. Applied at creation.
	// +optional
	Description string `json:"description,omitempty"`
	// OS is the operating system family, e.g. debian. Applied at creation.
	OS string `json:"os"`
	// Version is the operating system version, e.g. 12. Applied at creation.
	Version string `json:"version"`
	// URL of a raw disk image to import. It's written to a temporary disk that is snapshotted,
	// and the image is created from the snapshot. The disk and snapshot are then deleted.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="url is immutable"
	// +optional
	URL string `json:"url,omitempty"`
	// SHA256 is the expected hex SHA-256 digest of the file at url. The import fails on a mismatch.
	// +kubebuilder:validation:Pattern=`^[a-f0-9]{64}$`
	// +optional
	SHA256 string `json:"sha256,omitempty"`
	// Snapshot is the name or ID of a snapshot to create the image from.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="snapshot is immutable"
	// +optional
	Snapshot string `json:"snapshot,omitempty"`
	// BlockSize in bytes of an image imported from url.
	// +kubebuilder:validation:Enum=512;2048;4096
	// +kubebuilder:default=512
	// +optional
	BlockSize int `json:"blockSize,omitempty"`
	// Promote makes the image available to every project in the silo. Requires silo admin.
	// +optional
	Promote bool `json:"promote,omitempty"`
}

// ImageStatus defines the observed state of Image.
type ImageStatus struct {
	ResourceStatus `json:",inline"`
	// Size of the image.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oximg
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.project`
// +kubebuilder:printcolumn:name="OS",type=string,JSONPath=`.spec.os`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.status.size`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Image is the Schema for the images API. The object name is the Oxide image name.
type Image struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpec   `json:"spec"`
	Status ImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageList contains a list of Image.
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Image{}, &ImageList{})
		return nil
	})
}
