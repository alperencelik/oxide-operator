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
	"cmp"
	"context"
	"errors"
	"strings"

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

// VpcReconciler reconciles a Vpc object
type VpcReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=vpcs/finalizers,verbs=update

// Reconcile creates, updates and deletes the Oxide VPC behind a Vpc, including its firewall rules.
func (r *VpcReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	vpc := &oxidev1alpha1.Vpc{}
	if err := r.Get(ctx, req.NamespacedName, vpc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, vpc) {
		return ctrl.Result{}, nil
	}
	if !vpc.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, vpc)
	}
	if err := r.handleFinalizer(ctx, vpc); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(vpc.DeepCopy())
	res, err := setReady(&vpc.Status.Conditions, r.handleVpcOperations(ctx, vpc))
	if meta.IsStatusConditionTrue(vpc.Status.Conditions, typeReady) {
		vpc.Status.ObservedGeneration = vpc.Generation
	}
	if perr := r.Status().Patch(ctx, vpc, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleVpcOperations creates the VPC if it's missing, keeps its DNS name and description
// in sync and, when set, replaces its firewall rules.
func (r *VpcReconciler) handleVpcOperations(ctx context.Context, vpc *oxidev1alpha1.Vpc) error {
	logger := log.FromContext(ctx)
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, vpc.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	project, name := oxide.NameOrId(vpc.Spec.ProjectName()), oxide.NameOrId(vpc.Spec.OxideName(vpc))
	dnsName := oxide.Name(cmp.Or(vpc.Spec.DNSName, vpc.Spec.OxideName(vpc)))

	cur, err := oc.VpcView(ctx, oxide.VpcViewParams{Project: project, Vpc: name})
	switch {
	case errors.Is(err, oxide.ErrObjectNotFound):
		logger.Info("Creating Oxide VPC")
		cur, err = oc.VpcCreate(ctx, oxide.VpcCreateParams{Project: project, Body: &oxide.VpcCreate{
			Name: oxide.Name(name), Description: vpc.Spec.Description, DnsName: dnsName, Ipv6Prefix: oxide.Ipv6Net(vpc.Spec.IPv6Prefix),
		}})
	case err == nil && (cur.DnsName != dnsName || vpc.Spec.Description != "" && cur.Description != vpc.Spec.Description):
		logger.Info("Updating Oxide VPC")
		cur, err = oc.VpcUpdate(ctx, oxide.VpcUpdateParams{Project: project, Vpc: name, Body: &oxide.VpcUpdate{
			Description: vpc.Spec.Description, DnsName: dnsName,
		}})
	}
	if err != nil {
		return err
	}
	vpc.Status.ID = cur.Id

	if vpc.Spec.FirewallRules == nil {
		return nil
	}
	_, err = oc.VpcFirewallRulesUpdate(ctx, oxide.VpcFirewallRulesUpdateParams{
		Project: project, Vpc: name, Body: &oxide.VpcFirewallRuleUpdateParams{Rules: firewallRules(*vpc.Spec.FirewallRules)},
	})
	return err
}

func (r *VpcReconciler) handleFinalizer(ctx context.Context, vpc *oxidev1alpha1.Vpc) error {
	if controllerutil.AddFinalizer(vpc, finalizerName) {
		return r.Update(ctx, vpc)
	}
	return nil
}

// handleDelete deletes the Oxide VPC, unless protected, and removes the finalizer.
func (r *VpcReconciler) handleDelete(ctx context.Context, vpc *oxidev1alpha1.Vpc) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(vpc, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !vpc.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, vpc.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = deleteVpc(ctx, oc, oxide.NameOrId(vpc.Spec.ProjectName()), oxide.NameOrId(vpc.Spec.OxideName(vpc)))
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide VPC")
	}
	controllerutil.RemoveFinalizer(vpc, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, vpc))
}

// deleteVpc deletes a VPC together with the "default" subnet Oxide creates in every VPC,
// since Oxide won't delete a VPC that still has subnets.
func deleteVpc(ctx context.Context, oc *oxide.Client, project, vpc oxide.NameOrId) error {
	err := oc.VpcSubnetDelete(ctx, oxide.VpcSubnetDeleteParams{Project: project, Vpc: vpc, Subnet: "default"})
	if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
		return err
	}
	return oc.VpcDelete(ctx, oxide.VpcDeleteParams{Project: project, Vpc: vpc})
}

// firewallRules converts spec rules to the Oxide API shape. Values are already validated by the CRD.
func firewallRules(in []oxidev1alpha1.FirewallRule) []oxide.VpcFirewallRuleUpdate {
	out := make([]oxide.VpcFirewallRuleUpdate, 0, len(in))
	for _, r := range in {
		rule := oxide.VpcFirewallRuleUpdate{
			Name:        oxide.Name(r.Name),
			Description: r.Description,
			Action:      oxide.VpcFirewallRuleAction(r.Action),
			Direction:   oxide.VpcFirewallRuleDirection(r.Direction),
			Priority:    new(r.Priority),
			Status:      oxide.VpcFirewallRuleStatus(r.Status),
			Targets:     make([]oxide.VpcFirewallRuleTarget, 0, len(r.Targets)),
		}
		for _, t := range r.Targets {
			rule.Targets = append(rule.Targets, firewallTarget(t))
		}
		for _, h := range r.Filters.Hosts {
			rule.Filters.Hosts = append(rule.Filters.Hosts, firewallHost(h))
		}
		for _, p := range r.Filters.Ports {
			rule.Filters.Ports = append(rule.Filters.Ports, oxide.L4PortRange(p))
		}
		for _, p := range r.Filters.Protocols {
			rule.Filters.Protocols = append(rule.Filters.Protocols, firewallProtocol(p))
		}
		out = append(out, rule)
	}
	return out
}

func firewallTarget(t oxidev1alpha1.FirewallTarget) oxide.VpcFirewallRuleTarget {
	switch t.Type {
	case "vpc":
		return oxide.VpcFirewallRuleTarget{Value: &oxide.VpcFirewallRuleTargetVpc{Value: oxide.Name(t.Value)}}
	case "subnet":
		return oxide.VpcFirewallRuleTarget{Value: &oxide.VpcFirewallRuleTargetSubnet{Value: oxide.Name(t.Value)}}
	case "instance":
		return oxide.VpcFirewallRuleTarget{Value: &oxide.VpcFirewallRuleTargetInstance{Value: oxide.Name(t.Value)}}
	case "ip":
		return oxide.VpcFirewallRuleTarget{Value: &oxide.VpcFirewallRuleTargetIp{Value: t.Value}}
	}
	return oxide.VpcFirewallRuleTarget{Value: &oxide.VpcFirewallRuleTargetIpNet{Value: ipNet(t.Value)}}
}

func firewallHost(t oxidev1alpha1.FirewallTarget) oxide.VpcFirewallRuleHostFilter {
	switch t.Type {
	case "vpc":
		return oxide.VpcFirewallRuleHostFilter{Value: &oxide.VpcFirewallRuleHostFilterVpc{Value: oxide.Name(t.Value)}}
	case "subnet":
		return oxide.VpcFirewallRuleHostFilter{Value: &oxide.VpcFirewallRuleHostFilterSubnet{Value: oxide.Name(t.Value)}}
	case "instance":
		return oxide.VpcFirewallRuleHostFilter{Value: &oxide.VpcFirewallRuleHostFilterInstance{Value: oxide.Name(t.Value)}}
	case "ip":
		return oxide.VpcFirewallRuleHostFilter{Value: &oxide.VpcFirewallRuleHostFilterIp{Value: t.Value}}
	}
	return oxide.VpcFirewallRuleHostFilter{Value: &oxide.VpcFirewallRuleHostFilterIpNet{Value: ipNet(t.Value)}}
}

func ipNet(cidr string) oxide.IpNet {
	if strings.Contains(cidr, ":") {
		return oxide.IpNet{Value: oxide.Ipv6Net(cidr)}
	}
	return oxide.IpNet{Value: oxide.Ipv4Net(cidr)}
}

// ponytail: icmp/icmp6 match all ICMP types; add type/code filters when a rule needs them.
func firewallProtocol(p string) oxide.VpcFirewallRuleProtocol {
	switch p {
	case "udp":
		return oxide.VpcFirewallRuleProtocol{Value: &oxide.VpcFirewallRuleProtocolUdp{}}
	case "icmp":
		return oxide.VpcFirewallRuleProtocol{Value: &oxide.VpcFirewallRuleProtocolIcmp{}}
	case "icmp6":
		return oxide.VpcFirewallRuleProtocol{Value: &oxide.VpcFirewallRuleProtocolIcmp6{}}
	}
	return oxide.VpcFirewallRuleProtocol{Value: &oxide.VpcFirewallRuleProtocolTcp{}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *VpcReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Vpc{}, builder.WithPredicates(changed)).
		Named("oxide-vpc").
		Complete(r)
}
