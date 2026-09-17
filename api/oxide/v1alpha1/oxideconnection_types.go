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

// OxideConnectionSpec defines how to reach an Oxide silo.
type OxideConnectionSpec struct {
	// Host is the silo API URL, e.g. https://oxide.sys.example.com.
	// +kubebuilder:validation:Pattern=`^https?://`
	Host string `json:"host"`
	// TokenSecretRef selects the Secret key holding the Oxide API token.
	// The token is read when the connection is first used and again only when this spec changes,
	// so after replacing it in the Secret, change the spec or restart the operator.
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef"`
	// InsecureSkipVerify disables TLS certificate verification.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// SecretKeyReference selects a key of a Secret in a given namespace.
type SecretKeyReference struct {
	// Name of the Secret.
	Name string `json:"name"`
	// Namespace of the Secret.
	Namespace string `json:"namespace"`
	// Key within the Secret.
	// +kubebuilder:default=token
	// +optional
	Key string `json:"key,omitempty"`
}

// OxideConnectionStatus defines the observed state of OxideConnection.
type OxideConnectionStatus struct {
	// Silo is the silo the token authenticates against.
	// +optional
	Silo string `json:"silo,omitempty"`
	// User is the display name of the token's user.
	// +optional
	User string `json:"user,omitempty"`
	// Conditions describe the health of the connection.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=oxc
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.host`
// +kubebuilder:printcolumn:name="Silo",type=string,JSONPath=`.status.silo`
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=`.status.user`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// OxideConnection is the Schema for the oxideconnections API.
type OxideConnection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OxideConnectionSpec   `json:"spec"`
	Status OxideConnectionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OxideConnectionList contains a list of OxideConnection.
type OxideConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OxideConnection `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &OxideConnection{}, &OxideConnectionList{})
		return nil
	})
}
