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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

const (
	finalizerName = "oxide.100vms.com/finalizer"
	typeReady     = "Ready"

	// resyncPeriod re-reads Oxide to catch changes made outside Kubernetes.
	resyncPeriod = 10 * time.Minute
	// progressPeriod is how soon an in-flight Oxide operation is checked again.
	progressPeriod = 10 * time.Second
)

// progressing is returned while an Oxide operation is underway. setReady marks the
// resource not ready and requeues it after progressPeriod, without error backoff.
type progressing string

func (p progressing) Error() string { return string(p) }

// failed wraps an error retrying can't fix, like a bad spec.url. setReady marks the
// resource failed and stops retrying until the object changes.
type failed struct{ error }

func (f failed) Unwrap() error { return f.error }

// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// record emits a Normal event on obj for an Oxide change, unless it failed: failures show on the Ready condition.
func record(rec events.EventRecorder, obj runtime.Object, err error, reason, note string, args ...any) {
	if err == nil {
		rec.Eventf(obj, nil, corev1.EventTypeNormal, reason, reason, note, args...)
	}
}

// warn emits a Warning event on obj for a failed reconcile and returns err. Update conflicts are
// routine and skipped.
func warn(rec events.EventRecorder, obj runtime.Object, reason string, err error) error {
	if err != nil && !apierrors.IsConflict(err) {
		rec.Eventf(obj, nil, corev1.EventTypeWarning, reason, "Reconcile", "%s", err.Error())
	}
	return err
}

// reconcileDisabled reports whether the resource opted out with the reconcile-mode annotation, logging it if so.
func reconcileDisabled(ctx context.Context, obj client.Object) bool {
	if obj.GetAnnotations()[oxidev1alpha1.ReconcileModeAnnotation] != oxidev1alpha1.ReconcileModeDisable {
		return false
	}
	log.FromContext(ctx).Info("Reconcile mode set to disabled for object", "annotation", oxidev1alpha1.ReconcileModeAnnotation)
	return true
}

// setReady records the outcome of a reconcile on the Ready condition and decides when
// to requeue: soon while progressing, with backoff on error, never on a terminal error
// (until the object changes), and on resync otherwise.
// Oxide API errors are shortened to one line for both the condition and the returned error, and
// failures are also emitted as a Warning event on obj.
func setReady(rec events.EventRecorder, obj runtime.Object, conditions *[]metav1.Condition, err error) (ctrl.Result, error) {
	cond := metav1.Condition{Type: typeReady, Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "In sync with Omicron"}
	res := ctrl.Result{RequeueAfter: resyncPeriod}
	var p progressing
	var f failed
	switch {
	case errors.As(err, &f):
		// Checked first: a failure may be joined with a progressing cleanup.
		err = oxideclient.ShortError(err)
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Failed", err.Error()
		res, err = ctrl.Result{}, reconcile.TerminalError(err)
	case errors.As(err, &p):
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Progressing", err.Error()
		res, err = ctrl.Result{RequeueAfter: progressPeriod}, nil
	case err != nil:
		err = oxideclient.ShortError(err)
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Error", err.Error()
		res = ctrl.Result{}
	}
	meta.SetStatusCondition(conditions, cond)
	if err != nil {
		_ = warn(rec, obj, cond.Reason, errors.New(cond.Message)) // The message, without the "terminal error: " prefix.
	}
	return res, err
}
