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
	"context"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

const (
	finalizerName = "oxide.100vms.com/finalizer"
	typeReady     = "Ready"

	// resyncPeriod re-reads Oxide to catch changes made outside Kubernetes.
	resyncPeriod = 10 * time.Minute
	// progressPeriod is how soon an in-flight Oxide operation is checked again.
	progressPeriod = 5 * time.Second
)

// progressing is returned while an Oxide operation is underway. setReady marks the
// resource not ready and requeues it after progressPeriod, without error backoff.
type progressing string

func (p progressing) Error() string { return string(p) }

// reconcileDisabled reports whether the resource opted out with the reconcile-mode annotation, logging it if so.
func reconcileDisabled(ctx context.Context, obj client.Object) bool {
	if obj.GetAnnotations()[oxidev1alpha1.ReconcileModeAnnotation] != oxidev1alpha1.ReconcileModeDisable {
		return false
	}
	log.FromContext(ctx).Info("Reconcile mode set to disabled for object", "annotation", oxidev1alpha1.ReconcileModeAnnotation)
	return true
}

// setReady records the outcome of a reconcile on the Ready condition and decides when
// to requeue: soon while progressing, with backoff on error, and on resync otherwise.
// Oxide API errors are shortened to one line for both the condition and the returned error.
func setReady(conditions *[]metav1.Condition, err error) (ctrl.Result, error) {
	cond := metav1.Condition{Type: typeReady, Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "In sync with Omicron"}
	res := ctrl.Result{RequeueAfter: resyncPeriod}
	var p progressing
	switch {
	case errors.As(err, &p):
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Progressing", err.Error()
		res, err = ctrl.Result{RequeueAfter: progressPeriod}, nil
	case err != nil:
		err = oxideclient.ShortError(err)
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Error", err.Error()
		res = ctrl.Result{}
	}
	meta.SetStatusCondition(conditions, cond)
	return res, err
}
