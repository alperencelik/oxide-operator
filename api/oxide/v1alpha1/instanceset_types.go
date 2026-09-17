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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// InstanceSetSpec defines the desired state of InstanceSet.
type InstanceSetSpec struct {
	// Replicas is the number of Instances, named <set>-0 ... <set>-N. A template spec.name
	// is suffixed the same way.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// Template is copied into every Instance.
	Template InstanceTemplate `json:"template"`
}

// InstanceTemplate describes the Instances an InstanceSet creates.
type InstanceTemplate struct {
	// Labels added to each Instance.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Spec of each Instance.
	Spec InstanceSpec `json:"spec"`
}

// InstanceSetStatus defines the observed state of InstanceSet.
type InstanceSetStatus struct {
	// Replicas is the number of Instances managed by the set.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// ReadyReplicas is the number of Instances with Ready=True.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// Conditions describe the current state of the set.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last generation reconciled successfully.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas
// +kubebuilder:resource:shortName=oxis
// +kubebuilder:validation:XValidation:rule="!has(self.spec.template.spec.name) || size(self.spec.template.spec.name) <= 57",message="template spec.name is a prefix of at most 57 characters"
// +kubebuilder:validation:XValidation:rule="has(self.spec.template.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,55}[a-z0-9])?$')",message="name must be a valid Oxide name prefix: lowercase letters, digits and '-', starting with a letter, at most 57 characters"
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InstanceSet is the Schema for the instancesets API.
type InstanceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstanceSetSpec   `json:"spec"`
	Status InstanceSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstanceSetList contains a list of InstanceSet.
type InstanceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InstanceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &InstanceSet{}, &InstanceSetList{})
		return nil
	})
}
