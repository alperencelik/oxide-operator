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
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
)

func TestSetReady(t *testing.T) {
	var conds []metav1.Condition
	rec, obj := events.NewFakeRecorder(10), &oxidev1alpha1.Image{}
	res, err := setReady(rec, obj, &conds, errors.Join(failed{errors.New("bad sha")}, progressing("disk is finalizing")))
	if !errors.Is(err, reconcile.TerminalError(nil)) || res.RequeueAfter != 0 || conds[0].Reason != "Failed" {
		t.Errorf("failure joined with progressing: got %v, %v, %s; want terminal", res, err, conds[0].Reason)
	}
	if got, want := <-rec.Events, "Warning Failed bad sha\ndisk is finalizing"; got != want {
		t.Errorf("failure event: got %q, want %q", got, want)
	}
	res, err = setReady(rec, obj, &conds, progressing("disk is finalizing"))
	if err != nil || res.RequeueAfter != progressPeriod || conds[0].Reason != "Progressing" {
		t.Errorf("progressing: got %v, %v, %s", res, err, conds[0].Reason)
	}
	if len(rec.Events) != 0 {
		t.Errorf("progressing emitted event %q", <-rec.Events)
	}
}
