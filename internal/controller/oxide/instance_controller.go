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
	"encoding/base64"
	"errors"

	"github.com/alperencelik/kube-external-watcher/watcher"
	"github.com/oxidecomputer/oxide.go/oxide"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

// InstanceReconciler reconciles a Instance object
type InstanceReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Watcher *watcher.ExternalWatcher
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=instances/finalizers,verbs=update

// Reconcile creates, resizes, powers and deletes the Oxide instance behind an Instance.
func (r *InstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	inst := &oxidev1alpha1.Instance{}
	if err := r.Get(ctx, req.NamespacedName, inst); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, inst) {
		return ctrl.Result{}, nil
	}
	log.FromContext(ctx).Info("Reconciling Instance")
	if !inst.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, inst)
	}
	if err := r.handleFinalizer(ctx, inst); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(inst.DeepCopy())
	res, err := setReady(&inst.Status.Conditions, r.handleInstanceOperations(ctx, inst))
	if meta.IsStatusConditionTrue(inst.Status.Conditions, typeReady) {
		inst.Status.ObservedGeneration = inst.Generation
	}
	if perr := r.Status().Patch(ctx, inst, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleInstanceOperations creates the instance if it's missing, then converges its size and power state.
func (r *InstanceReconciler) handleInstanceOperations(ctx context.Context, inst *oxidev1alpha1.Instance) error {
	logger := log.FromContext(ctx)
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, inst.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	project, name := oxide.NameOrId(inst.Spec.ProjectName()), oxide.NameOrId(inst.Spec.OxideName(inst))
	cur, err := oc.InstanceView(ctx, oxide.InstanceViewParams{Project: project, Instance: name})
	if errors.Is(err, oxide.ErrObjectNotFound) {
		logger.Info("Creating Oxide instance")
		body, berr := instanceCreate(ctx, oc, inst.Spec.ProjectName(), inst)
		if berr != nil {
			return berr
		}
		cur, err = oc.InstanceCreate(ctx, oxide.InstanceCreateParams{Project: project, Body: body})
	}
	if err != nil {
		return err
	}
	inst.Status.ID, inst.Status.Project = cur.Id, inst.Spec.ProjectName()
	inst.Status.State = string(cur.RunState)

	// Oxide only resizes stopped instances.
	if int(cur.Ncpus) != inst.Spec.NCPUs || int64(cur.Memory) != inst.Spec.Memory.Value() {
		if cur.RunState != oxide.InstanceStateStopped {
			return stopInstance(ctx, oc, cur, "stopping instance to resize")
		}
		logger.Info("Resizing Oxide instance", "ncpus", inst.Spec.NCPUs, "memory", inst.Spec.Memory.String())
		_, err := oc.InstanceUpdate(ctx, oxide.InstanceUpdateParams{Instance: oxide.NameOrId(cur.Id), Body: instanceUpdate(cur, inst)})
		if err != nil {
			return err
		}
		return progressing("resized instance")
	}

	wantRunning := inst.Spec.RunState != oxidev1alpha1.RunStateStopped
	switch {
	case cur.RunState == oxide.InstanceStateStopped && wantRunning:
		logger.Info("Starting Oxide instance")
		if _, err := oc.InstanceStart(ctx, oxide.InstanceStartParams{Instance: oxide.NameOrId(cur.Id)}); err != nil {
			return err
		}
		return progressing("starting instance")
	case cur.RunState == oxide.InstanceStateRunning && !wantRunning:
		return stopInstance(ctx, oc, cur, "stopping instance")
	case cur.RunState == oxide.InstanceStateFailed:
		return errors.New("instance is in failed state")
	case cur.RunState != oxide.InstanceStateRunning && cur.RunState != oxide.InstanceStateStopped:
		return progressing("instance is " + string(cur.RunState))
	}

	ips, err := oc.InstanceExternalIpList(ctx, oxide.InstanceExternalIpListParams{Instance: oxide.NameOrId(cur.Id)})
	if err != nil {
		return err
	}
	inst.Status.ExternalIPs = nil
	for _, ip := range ips.Items {
		switch v := ip.Value.(type) {
		case *oxide.ExternalIpEphemeral:
			inst.Status.ExternalIPs = append(inst.Status.ExternalIPs, v.Ip)
		case *oxide.ExternalIpFloating:
			inst.Status.ExternalIPs = append(inst.Status.ExternalIPs, v.Ip)
		}
	}
	now := metav1.Now()
	inst.Status.LastObserved = &now
	return nil
}

func (r *InstanceReconciler) handleFinalizer(ctx context.Context, inst *oxidev1alpha1.Instance) error {
	if controllerutil.AddFinalizer(inst, finalizerName) {
		return r.Update(ctx, inst)
	}
	return nil
}

// handleDelete stops and deletes the Oxide instance, unless protected, and removes the finalizer.
func (r *InstanceReconciler) handleDelete(ctx context.Context, inst *oxidev1alpha1.Instance) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(inst, finalizerName) {
		return ctrl.Result{}, nil
	}
	// Stop watching before deleting since the delete can't be reverted.
	r.Watcher.Unregister(client.ObjectKeyFromObject(inst))

	if !inst.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, inst.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		var p progressing
		switch err := deleteInstance(ctx, oc, oxide.NameOrId(inst.Spec.ProjectName()), inst); {
		case errors.As(err, &p):
			return ctrl.Result{RequeueAfter: progressPeriod}, nil
		case err != nil:
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide instance")
	}
	controllerutil.RemoveFinalizer(inst, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, inst))
}

// deleteInstance stops and deletes the instance, then removes the boot disk created with it.
func deleteInstance(ctx context.Context, oc *oxide.Client, project oxide.NameOrId, inst *oxidev1alpha1.Instance) error {
	cur, err := oc.InstanceView(ctx, oxide.InstanceViewParams{Project: project, Instance: oxide.NameOrId(inst.Spec.OxideName(inst))})
	switch {
	case errors.Is(err, oxide.ErrObjectNotFound):
	case err != nil:
		return err
	case cur.RunState == oxide.InstanceStateStopped || cur.RunState == oxide.InstanceStateFailed:
		err := oc.InstanceDelete(ctx, oxide.InstanceDeleteParams{Instance: oxide.NameOrId(cur.Id)})
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return err
		}
	default:
		return stopInstance(ctx, oc, cur, "stopping instance before delete")
	}
	if bd := inst.Spec.BootDisk; bd != nil && bd.Disk == "" {
		err := oc.DiskDelete(ctx, oxide.DiskDeleteParams{Project: project, Disk: oxide.NameOrId(bootDiskName(inst))})
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return err
		}
	}
	return nil
}

func stopInstance(ctx context.Context, oc *oxide.Client, cur *oxide.Instance, msg string) error {
	if cur.RunState == oxide.InstanceStateRunning {
		log.FromContext(ctx).Info("Stopping Oxide instance", "reason", msg)
		if _, err := oc.InstanceStop(ctx, oxide.InstanceStopParams{Instance: oxide.NameOrId(cur.Id)}); err != nil {
			return err
		}
	}
	return progressing(msg)
}

func bootDiskName(inst *oxidev1alpha1.Instance) string { return inst.Spec.OxideName(inst) + "-boot" }

func instanceCreate(ctx context.Context, oc *oxide.Client, project string, inst *oxidev1alpha1.Instance) (*oxide.InstanceCreate, error) {
	spec, name := inst.Spec, inst.Spec.OxideName(inst)
	body := &oxide.InstanceCreate{
		Name:        oxide.Name(name),
		Description: spec.Description,
		Hostname:    oxide.Hostname(cmp.Or(spec.Hostname, name)),
		Ncpus:       oxide.InstanceCpuCount(spec.NCPUs),
		Memory:      oxide.ByteCount(spec.Memory.Value()),
		Start:       new(spec.RunState != oxidev1alpha1.RunStateStopped),
		UserData:    base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
	}
	if bd := spec.BootDisk; bd != nil && bd.Disk != "" {
		body.BootDisk = oxide.InstanceDiskAttachment{Value: &oxide.InstanceDiskAttachmentAttach{Name: oxide.Name(bd.Disk)}}
	} else if bd != nil {
		src, err := diskSource(ctx, oc, project, bd.Image, "", 4096)
		if err != nil {
			return nil, err
		}
		body.BootDisk = oxide.InstanceDiskAttachment{Value: &oxide.InstanceDiskAttachmentCreate{
			Name:        oxide.Name(bootDiskName(inst)),
			Description: "Boot disk for instance " + name,
			Size:        oxide.ByteCount(bd.Size.Value()),
			DiskBackend: oxide.DiskBackend{Value: &oxide.DiskBackendDistributed{DiskSource: src}},
		}}
	}
	for _, d := range spec.Disks {
		body.Disks = append(body.Disks, oxide.InstanceDiskAttachment{Value: &oxide.InstanceDiskAttachmentAttach{Name: oxide.Name(d)}})
	}
	if len(spec.NetworkInterfaces) > 0 {
		nics := make([]oxide.InstanceNetworkInterfaceCreate, 0, len(spec.NetworkInterfaces))
		for _, n := range spec.NetworkInterfaces {
			nics = append(nics, oxide.InstanceNetworkInterfaceCreate{
				Name: oxide.Name(n.Name), Description: n.Description, VpcName: oxide.Name(n.Vpc), SubnetName: oxide.Name(n.Subnet),
			})
		}
		body.NetworkInterfaces = oxide.InstanceNetworkInterfaceAttachment{Value: &oxide.InstanceNetworkInterfaceAttachmentCreate{Params: nics}}
	}
	for _, ip := range spec.ExternalIPs {
		if ip.Type == oxidev1alpha1.ExternalIPFloating {
			body.ExternalIps = append(body.ExternalIps, oxide.ExternalIpCreate{Value: &oxide.ExternalIpCreateFloating{FloatingIp: oxide.NameOrId(ip.FloatingIP)}})
			continue
		}
		eph := &oxide.ExternalIpCreateEphemeral{}
		if ip.Pool != "" {
			eph.PoolSelector = oxide.PoolSelector{Value: &oxide.PoolSelectorExplicit{Pool: oxide.NameOrId(ip.Pool)}}
		}
		body.ExternalIps = append(body.ExternalIps, oxide.ExternalIpCreate{Value: eph})
	}
	for _, k := range spec.SSHPublicKeys {
		body.SshPublicKeys = append(body.SshPublicKeys, oxide.NameOrId(k))
	}
	return body, nil
}

// instanceUpdate builds a resize request. InstanceUpdate replaces every field, so the
// current boot disk, restart policy, CPU platform and jumbo frames are carried over.
func instanceUpdate(cur *oxide.Instance, inst *oxidev1alpha1.Instance) *oxide.InstanceUpdate {
	u := &oxide.InstanceUpdate{
		Ncpus:             oxide.InstanceCpuCount(inst.Spec.NCPUs),
		Memory:            oxide.ByteCount(inst.Spec.Memory.Value()),
		EnableJumboFrames: cur.EnableJumboFrames,
	}
	if cur.BootDiskId != "" {
		u.BootDisk = new(oxide.NameOrId(cur.BootDiskId))
	}
	if cur.AutoRestartPolicy != "" {
		u.AutoRestartPolicy = new(cur.AutoRestartPolicy)
	}
	if cur.CpuPlatform != "" {
		u.CpuPlatform = new(cur.CpuPlatform)
	}
	return u
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Instance{}, builder.WithPredicates(changed)).
		WatchesRawSource(r.Watcher).
		Named("oxide-instance").
		Complete(r)
}
