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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// VpcSubnetSpec defines the desired state of an Oxide VPC subnet.
// +kubebuilder:validation:XValidation:rule="has(self.vpc) != has(self.vpcRef)",message="exactly one of vpc or vpcRef must be set"
// +kubebuilder:validation:XValidation:rule="has(self.vpcRef) ? !has(self.project) && !has(self.projectRef) : has(self.project) != has(self.projectRef)",message="vpc requires exactly one of project or projectRef; vpcRef takes the project from the Vpc"
// +kubebuilder:validation:XValidation:rule="has(self.vpcRef) == has(oldSelf.vpcRef)",message="vpc is immutable"
type VpcSubnetSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Vpc is the name of the VPC.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vpc is immutable"
	// +optional
	Vpc string `json:"vpc,omitempty"`
	// VpcRef references a Vpc in the same namespace, which also sets the project.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vpcRef is immutable"
	// +optional
	VpcRef *corev1.LocalObjectReference `json:"vpcRef,omitempty"`
	// Description of the subnet.
	// +optional
	Description string `json:"description,omitempty"`
	// IPv4Block is the subnet's IPv4 CIDR, e.g. 172.30.0.0/22.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ipv4Block is immutable"
	IPv4Block string `json:"ipv4Block"`
	// IPv6Block is the subnet's IPv6 /64. Assigned by Oxide if unset.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ipv6Block is immutable"
	// +optional
	IPv6Block string `json:"ipv6Block,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oxsubnet
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.project`
// +kubebuilder:printcolumn:name="VPC",type=string,JSONPath=`.spec.vpc`
// +kubebuilder:printcolumn:name="IPv4",type=string,JSONPath=`.spec.ipv4Block`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VpcSubnet is the Schema for the vpcsubnets API. The object name is the Oxide subnet name.
type VpcSubnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpcSubnetSpec  `json:"spec"`
	Status ResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VpcSubnetList contains a list of VpcSubnet.
type VpcSubnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VpcSubnet `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &VpcSubnet{}, &VpcSubnetList{})
		return nil
	})
}
