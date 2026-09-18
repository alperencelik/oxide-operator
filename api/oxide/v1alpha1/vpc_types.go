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

// VpcSpec defines the desired state of an Oxide VPC.
// +kubebuilder:validation:XValidation:rule="has(self.project) != has(self.projectRef)",message="exactly one of project or projectRef must be set"
type VpcSpec struct {
	ResourceSpec   `json:",inline"`
	ProjectRefSpec `json:",inline"`
	OxideNameSpec  `json:",inline"`
	// Description of the VPC.
	// +optional
	Description string `json:"description,omitempty"`
	// DNSName defaults to the object name.
	// +optional
	DNSName string `json:"dnsName,omitempty"`
	// IPv6Prefix is the VPC's IPv6 /48. Assigned by Oxide if unset.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ipv6Prefix is immutable"
	// +optional
	IPv6Prefix string `json:"ipv6Prefix,omitempty"`
	// FirewallRules replaces all of the VPC's firewall rules when set, including the
	// defaults Oxide creates. Leave unset to manage rules outside Kubernetes.
	// Values mirror the Oxide API.
	// +optional
	FirewallRules *[]FirewallRule `json:"firewallRules,omitempty"`
}

// FirewallRule is a VPC firewall rule.
type FirewallRule struct {
	// Name of the rule.
	Name string `json:"name"`
	// Description of the rule.
	// +optional
	Description string `json:"description,omitempty"`
	// Action taken on matching traffic.
	// +kubebuilder:validation:Enum=allow;deny
	Action string `json:"action"`
	// Direction of matching traffic.
	// +kubebuilder:validation:Enum=inbound;outbound
	Direction string `json:"direction"`
	// Priority of the rule; lower values win.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Priority int `json:"priority"`
	// Status enables or disables the rule.
	// +kubebuilder:validation:Enum=enabled;disabled
	// +kubebuilder:default=enabled
	// +optional
	Status string `json:"status,omitempty"`
	// Targets are the VPC resources the rule applies to.
	Targets []FirewallTarget `json:"targets"`
	// Filters narrow the traffic the rule matches.
	// +optional
	Filters FirewallFilter `json:"filters,omitempty"`
}

// FirewallTarget identifies a VPC, subnet, instance, IP or IP network.
type FirewallTarget struct {
	// Type of the target.
	// +kubebuilder:validation:Enum=vpc;subnet;instance;ip;ip_net
	Type string `json:"type"`
	// Value is a name for vpc, subnet and instance, an address for ip, or a CIDR for ip_net.
	Value string `json:"value"`
}

// FirewallFilter narrows the traffic a rule matches. Empty fields match everything.
type FirewallFilter struct {
	// Hosts limits the rule to traffic to or from these hosts.
	// +optional
	Hosts []FirewallTarget `json:"hosts,omitempty"`
	// Ports are port numbers or ranges, e.g. 22 or 8000-8080.
	// +optional
	Ports []string `json:"ports,omitempty"`
	// Protocols to match.
	// +kubebuilder:validation:items:Enum=tcp;udp;icmp;icmp6
	// +optional
	Protocols []string `json:"protocols,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oxvpc
// +kubebuilder:validation:XValidation:rule="has(self.spec.name) || self.metadata.name.matches('^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$')",message="name must be a valid Oxide name: lowercase letters, digits and '-', starting with a letter, at most 63 characters"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.status.project`
// +kubebuilder:printcolumn:name="ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Vpc is the Schema for the vpcs API. The object name is the Oxide VPC name.
type Vpc struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpcSpec        `json:"spec"`
	Status ResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VpcList contains a list of Vpc.
type VpcList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Vpc `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Vpc{}, &VpcList{})
		return nil
	})
}
