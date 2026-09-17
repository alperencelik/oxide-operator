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

package oxideclient

import (
	"context"
	"fmt"

	"github.com/alperencelik/kube-external-watcher/watcher"
	"github.com/oxidecomputer/oxide.go/oxide"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
)

// InstanceKey identifies an Oxide instance for the external watcher.
type InstanceKey struct {
	Connection string
	Project    string
	Name       string
}

// InstanceState is the part of an instance compared for drift.
type InstanceState struct {
	NCPUs    int
	Memory   int64
	RunState string
}

// InstanceFetcher implements watcher.ResourceStateFetcher and watcher.ResourceStatusUpdater for Instances.
type InstanceFetcher struct {
	Client client.Client
}

func (f *InstanceFetcher) GetDesiredState(ctx context.Context, key types.NamespacedName) (any, error) {
	inst := &oxidev1alpha1.Instance{}
	if err := f.Client.Get(ctx, key, inst); err != nil {
		return nil, err
	}
	if inst.Annotations[oxidev1alpha1.ReconcileModeAnnotation] == oxidev1alpha1.ReconcileModeDisable {
		return nil, nil
	}
	return InstanceState{NCPUs: inst.Spec.NCPUs, Memory: inst.Spec.Memory.Value(), RunState: inst.Spec.RunState}, nil
}

func (f *InstanceFetcher) FetchExternalResource(ctx context.Context, objKey any) (any, error) {
	key, ok := objKey.(InstanceKey)
	if !ok {
		return nil, fmt.Errorf("unexpected resource key type %T", objKey)
	}
	oc, err := NewClientFromRef(ctx, f.Client, key.Connection)
	if err != nil {
		return nil, err
	}
	inst, err := oc.InstanceView(ctx, oxide.InstanceViewParams{
		Project:  oxide.NameOrId(key.Project),
		Instance: oxide.NameOrId(key.Name),
	})
	return inst, ShortError(err)
}

func (f *InstanceFetcher) TransformExternalState(raw any) (any, error) {
	inst, ok := raw.(*oxide.Instance)
	if !ok {
		return nil, fmt.Errorf("unexpected external state type %T", raw)
	}
	return InstanceState{NCPUs: int(inst.Ncpus), Memory: int64(inst.Memory), RunState: runState(inst.RunState)}, nil
}

func (f *InstanceFetcher) IsResourceReadyToWatch(ctx context.Context, key types.NamespacedName) bool {
	inst := &oxidev1alpha1.Instance{}
	return f.Client.Get(ctx, key, inst) == nil && inst.Status.ID != ""
}

func (f *InstanceFetcher) UpdateResourceStatus(ctx context.Context, key types.NamespacedName, raw any) error {
	external, ok := raw.(*oxide.Instance)
	if !ok {
		return fmt.Errorf("unexpected external state type %T", raw)
	}
	inst := &oxidev1alpha1.Instance{}
	if err := f.Client.Get(ctx, key, inst); err != nil {
		return err
	}
	patch := client.MergeFrom(inst.DeepCopy())
	now := metav1.Now()
	inst.Status.State = string(external.RunState)
	inst.Status.LastObserved = &now
	return f.Client.Status().Patch(ctx, inst, patch)
}

// InstanceConfigExtractor builds the watcher registration for an Instance.
func InstanceConfigExtractor(obj client.Object) watcher.ResourceConfig {
	inst := obj.(*oxidev1alpha1.Instance)
	return watcher.ResourceConfig{ResourceKey: InstanceKey{
		Connection: inst.Spec.ConnectionRef.Name,
		Project:    inst.Spec.ProjectName(),
		Name:       inst.Spec.OxideName(inst),
	}}
}

// runState maps an Oxide run state onto the spec's Running/Stopped so in-flight
// transitions toward the desired state don't count as drift.
func runState(s oxide.InstanceState) string {
	switch s {
	case oxide.InstanceStateRunning, oxide.InstanceStateStarting, oxide.InstanceStateRebooting,
		oxide.InstanceStateMigrating:
		return oxidev1alpha1.RunStateRunning
	case oxide.InstanceStateStopped, oxide.InstanceStateStopping:
		return oxidev1alpha1.RunStateStopped
	}
	return string(s)
}
