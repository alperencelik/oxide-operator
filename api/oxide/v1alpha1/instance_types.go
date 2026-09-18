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

// InstanceSpec defines the desired state of an Oxide instance.
// Fields marked "applied at creation" are only used when the instance is created.
// +kubebuilder:validation:XValidation:rule="has(self.project) != has(self.projectRef)",message="exactly one of project or projectRef must be set"
type InstanceSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Description of the instance. Applied at creation.
	// +optional
	Description string `json:"description,omitempty"`
	// Hostname defaults to the Oxide name. Applied at creation.
	// +optional
	Hostname string `json:"hostname,omitempty"`
	// NCPUs is the number of vCPUs. Changing it restarts the instance.
	// +kubebuilder:validation:Minimum=1
	NCPUs int `json:"ncpus"`
	// Memory is the amount of RAM, e.g. 4Gi. Changing it restarts the instance.
	Memory resource.Quantity `json:"memory"`
	// RunState is the desired power state.
	// +kubebuilder:validation:Enum=Running;Stopped
	// +kubebuilder:default=Running
	// +optional
	RunState string `json:"runState,omitempty"`
	// BootDisk is created with the instance, or an existing disk to boot from. Immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bootDisk is immutable"
	// +optional
	BootDisk *BootDisk `json:"bootDisk,omitempty"`
	// Disks are names of existing disks to attach. Applied at creation.
	// +optional
	Disks []string `json:"disks,omitempty"`
	// NetworkInterfaces to create. Defaults to one interface in the project's default VPC.
	// Applied at creation.
	// +optional
	NetworkInterfaces []NetworkInterface `json:"networkInterfaces,omitempty"`
	// ExternalIPs to allocate. Applied at creation.
	// +optional
	ExternalIPs []ExternalIP `json:"externalIPs,omitempty"`
	// SSHPublicKeys are names or IDs of the user's SSH keys to inject. Defaults to all of them.
	// Applied at creation.
	// +optional
	SSHPublicKeys []string `json:"sshPublicKeys,omitempty"`
	// UserData is plain-text cloud-init user data. Applied at creation.
	// +optional
	UserData string `json:"userData,omitempty"`
}

// BootDisk describes the instance boot disk.
// +kubebuilder:validation:XValidation:rule="has(self.disk) != has(self.size)",message="set exactly one of disk or size"
// +kubebuilder:validation:XValidation:rule="!has(self.image) || has(self.size)",message="image requires size"
type BootDisk struct {
	// Size of a new boot disk named <instance>-boot, deleted with the instance.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`
	// Image is the name or ID of the image for the new boot disk. Blank if unset.
	// +optional
	Image string `json:"image,omitempty"`
	// Disk is the name of an existing disk to boot from.
	// +optional
	Disk string `json:"disk,omitempty"`
}

// NetworkInterface attaches the instance to a VPC subnet.
type NetworkInterface struct {
	// Name of the interface.
	Name string `json:"name"`
	// Description of the interface.
	// +optional
	Description string `json:"description,omitempty"`
	// Vpc is the VPC name.
	Vpc string `json:"vpc"`
	// Subnet is the subnet name within the VPC.
	Subnet string `json:"subnet"`
}

// ExternalIP is an external address for the instance.
// +kubebuilder:validation:XValidation:rule="(self.type == 'Floating') == has(self.floatingIP)",message="floatingIP is required for Floating and not allowed for Ephemeral"
type ExternalIP struct {
	// Type of the external IP.
	// +kubebuilder:validation:Enum=Ephemeral;Floating
	Type string `json:"type"`
	// Pool to allocate an ephemeral IP from. Defaults to the silo's default pool.
	// +optional
	Pool string `json:"pool,omitempty"`
	// FloatingIP is the name or ID of an existing floating IP.
	// +optional
	FloatingIP string `json:"floatingIP,omitempty"`
}

// InstanceStatus defines the observed state of Instance.
type InstanceStatus struct {
	ResourceStatus `json:",inline"`
	// State is the Oxide run state, e.g. running or stopped.
	// +optional
	State string `json:"state,omitempty"`
	// ExternalIPs are the instance's external IP addresses.
	// +optional
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// LastObserved is when the state was last read from Oxide.
	// +optional
	LastObserved *metav1.Time `json:"lastObserved,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oxi
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.status.project`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="CPUs",type=integer,JSONPath=`.spec.ncpus`
// +kubebuilder:printcolumn:name="Memory",type=string,JSONPath=`.spec.memory`
// +kubebuilder:printcolumn:name="External IP",type=string,JSONPath=`.status.externalIPs[0]`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Instance is the Schema for the instances API. The object name is the Oxide instance name.
type Instance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InstanceSpec   `json:"spec"`
	Status InstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InstanceList contains a list of Instance.
type InstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Instance `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Instance{}, &InstanceList{})
		return nil
	})
}
