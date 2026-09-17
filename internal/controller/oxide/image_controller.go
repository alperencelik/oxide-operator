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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/oxidecomputer/oxide.go/oxide"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
	"github.com/alperencelik/oxide-operator/pkg/oxideclient"
)

const (
	// importChunkSize is the largest bulk write the Oxide API accepts.
	importChunkSize = 512 * 1024
	gib             = 1 << 30
)

// ImageReconciler reconciles a Image object
type ImageReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=oxide.100vms.com,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=oxide.100vms.com,resources=images/finalizers,verbs=update

// Reconcile creates, promotes and deletes the Oxide image behind an Image.
func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	image := &oxidev1alpha1.Image{}
	if err := r.Get(ctx, req.NamespacedName, image); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if reconcileDisabled(ctx, image) {
		return ctrl.Result{}, nil
	}
	if !image.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, image)
	}
	if err := r.handleFinalizer(ctx, image); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(image.DeepCopy())
	res, err := setReady(&image.Status.Conditions, r.handleImageOperations(ctx, image))
	if meta.IsStatusConditionTrue(image.Status.Conditions, typeReady) {
		image.Status.ObservedGeneration = image.Generation
	}
	if perr := r.Status().Patch(ctx, image, patch); perr != nil && err == nil {
		return ctrl.Result{}, client.IgnoreNotFound(perr)
	}
	return res, err
}

// handleImageOperations creates the image if it's missing and promotes or demotes it to match the spec.
func (r *ImageReconciler) handleImageOperations(ctx context.Context, image *oxidev1alpha1.Image) error {
	oc, err := oxideclient.NewClientFromRef(ctx, r.Client, image.Spec.ConnectionRef.Name)
	if err != nil {
		return err
	}
	cur, err := viewImage(ctx, oc, image.Spec.ProjectName(), image.Spec.OxideName(image))
	if errors.Is(err, oxide.ErrObjectNotFound) {
		cur, err = createImage(ctx, oc, image)
	}
	if err != nil {
		return err
	}
	// A new image means its import is done. The ID is recorded only after cleanup so a failed cleanup is retried.
	if cur.Id != image.Status.ID {
		if err := deleteImport(ctx, oc, image); err != nil {
			return err
		}
	}
	image.Status.ID = cur.Id
	image.Status.Size = resource.NewQuantity(int64(cur.Size), resource.BinarySI)
	logger := log.FromContext(ctx)
	switch {
	case image.Spec.Promote && cur.ProjectId != "":
		logger.Info("Promoting Oxide image to silo")
		_, err = oc.ImagePromote(ctx, oxide.ImagePromoteParams{Image: oxide.NameOrId(cur.Id)})
	case !image.Spec.Promote && cur.ProjectId == "":
		logger.Info("Demoting Oxide image to project")
		_, err = oc.ImageDemote(ctx, oxide.ImageDemoteParams{
			Project: oxide.NameOrId(image.Spec.ProjectName()), Image: oxide.NameOrId(cur.Id),
		})
	}
	return err
}

// createImage creates the image from spec.snapshot, or from the import snapshot once spec.url is imported.
func createImage(ctx context.Context, oc *oxide.Client, image *oxidev1alpha1.Image) (*oxide.Image, error) {
	project, snapshot := oxide.NameOrId(image.Spec.ProjectName()), oxide.NameOrId(image.Spec.Snapshot)
	if image.Spec.URL != "" {
		snapshot = oxide.NameOrId(importName(image))
	}
	snap, err := oc.SnapshotView(ctx, oxide.SnapshotViewParams{Project: project, Snapshot: snapshot})
	if image.Spec.URL != "" && errors.Is(err, oxide.ErrObjectNotFound) {
		return nil, importDisk(ctx, oc, image)
	}
	if err != nil {
		return nil, err
	}
	if snap.State == oxide.SnapshotStateCreating {
		return nil, progressing("snapshot is creating")
	}
	log.FromContext(ctx).Info("Creating Oxide image")
	return oc.ImageCreate(ctx, oxide.ImageCreateParams{Project: project, Body: &oxide.ImageCreate{
		Name:        oxide.Name(image.Spec.OxideName(image)),
		Description: image.Spec.Description,
		Os:          image.Spec.OS,
		Version:     image.Spec.Version,
		Source:      oxide.ImageSource{Value: &oxide.ImageSourceSnapshot{Id: snap.Id}},
	}})
}

// importDisk moves the import of spec.url one step forward: create the import disk, write it and
// finalize it into the import snapshot.
func importDisk(ctx context.Context, oc *oxide.Client, image *oxidev1alpha1.Image) error {
	project, name := oxide.NameOrId(image.Spec.ProjectName()), importName(image)
	disk, err := oc.DiskView(ctx, oxide.DiskViewParams{Project: project, Disk: oxide.NameOrId(name)})
	if errors.Is(err, oxide.ErrObjectNotFound) {
		return createImportDisk(ctx, oc, image, name)
	}
	if err != nil {
		return err
	}
	switch state := disk.State.State(); state {
	case oxide.DiskStateStateImportReady:
		return writeDisk(ctx, oc, image, name, int64(disk.Size))
	case oxide.DiskStateStateImportingFromBulkWrites:
		// A write was interrupted, e.g. by an operator restart.
		if err := deleteImportDisk(ctx, oc, project, oxide.NameOrId(name)); err != nil {
			return err
		}
		return progressing("restarting interrupted import")
	case oxide.DiskStateStateCreating, oxide.DiskStateStateFinalizing:
		return progressing("import disk is " + string(state))
	default:
		return fmt.Errorf("import disk %s is %s", name, state)
	}
}

// createImportDisk creates a disk ready for bulk writes, sized to hold spec.url.
func createImportDisk(ctx context.Context, oc *oxide.Client, image *oxidev1alpha1.Image, name string) error {
	resp, err := download(ctx, image.Spec.URL)
	if err != nil {
		return err
	}
	_ = resp.Body.Close() // Only the length is needed now.
	log.FromContext(ctx).Info("Creating Oxide disk to import image", "url", image.Spec.URL)
	_, err = oc.DiskCreate(ctx, oxide.DiskCreateParams{Project: oxide.NameOrId(image.Spec.ProjectName()), Body: &oxide.DiskCreate{
		Name:        oxide.Name(name),
		Description: "Temporary disk for importing image " + image.Name,
		// Disk sizes are whole GiB.
		Size: oxide.ByteCount(max(gib, (resp.ContentLength+gib-1)/gib*gib)),
		DiskBackend: oxide.DiskBackend{Value: &oxide.DiskBackendDistributed{DiskSource: oxide.DiskSource{
			Value: &oxide.DiskSourceImportingBlocks{BlockSize: oxide.BlockSize(image.Spec.BlockSize)},
		}}},
	}})
	if err != nil {
		return err
	}
	return progressing("creating import disk")
}

// writeDisk writes spec.url to the import disk and finalizes the disk, which snapshots it.
//
// ponytail: runs inside Reconcile with sequential writes, so an import blocks the image controller
// until it's done; move it to a goroutine with parallel writes if imports get slow or many.
func writeDisk(ctx context.Context, oc *oxide.Client, image *oxidev1alpha1.Image, name string, size int64) error {
	resp, err := download(ctx, image.Spec.URL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.ContentLength > size {
		return fmt.Errorf("%s is larger than import disk %s", image.Spec.URL, name)
	}
	project, disk := oxide.NameOrId(image.Spec.ProjectName()), oxide.NameOrId(name)
	log.FromContext(ctx).Info("Importing Oxide image", "url", image.Spec.URL)
	if err := oc.DiskBulkWriteImportStart(ctx, oxide.DiskBulkWriteImportStartParams{Project: project, Disk: disk}); err != nil {
		return err
	}
	sum := sha256.New()
	err = writeChunks(io.TeeReader(resp.Body, sum), image.Spec.BlockSize, func(offset uint64, data []byte) error {
		return oc.DiskBulkWriteImport(ctx, oxide.DiskBulkWriteImportParams{Project: project, Disk: disk, Body: &oxide.ImportBlocksBulkWrite{
			Base64EncodedData: base64.StdEncoding.EncodeToString(data), Offset: &offset,
		}})
	})
	if err == nil && image.Spec.SHA256 != "" && hex.EncodeToString(sum.Sum(nil)) != image.Spec.SHA256 {
		err = fmt.Errorf("sha256 of %s does not match spec.sha256", image.Spec.URL)
	}
	if err == nil {
		err = oc.DiskBulkWriteImportStop(ctx, oxide.DiskBulkWriteImportStopParams{Project: project, Disk: disk})
	}
	if err != nil {
		// Retry on a fresh disk: skipped zero chunks are only correct on a disk that reads as zeros.
		return errors.Join(err, deleteImportDisk(ctx, oc, project, disk))
	}
	err = oc.DiskFinalizeImport(ctx, oxide.DiskFinalizeImportParams{
		Project: project, Disk: disk, Body: &oxide.FinalizeDisk{SnapshotName: oxide.Name(name)},
	})
	if err != nil {
		return err
	}
	return progressing("finalizing import")
}

// writeChunks reads r in bulk write sized chunks and writes those that aren't all zeros, since a new
// disk already reads as zeros. The last chunk is zero-padded to a whole block.
func writeChunks(r io.Reader, blockSize int, write func(offset uint64, data []byte) error) error {
	buf, zeros := make([]byte, importChunkSize), make([]byte, importChunkSize)
	for offset := uint64(0); ; offset += importChunkSize {
		n, err := io.ReadFull(r, buf)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		padded := (n + blockSize - 1) / blockSize * blockSize
		clear(buf[n:padded])
		if !bytes.Equal(buf[:padded], zeros[:padded]) {
			if werr := write(offset, buf[:padded]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return nil
		}
	}
}

// download GETs url, which must report its length. The caller closes the body.
func download(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("downloading %s: got %s with length %d, want 200 OK with a known length",
			url, resp.Status, resp.ContentLength)
	}
	return resp, nil
}

// importName names the temporary disk and snapshot an image is imported through. The UID suffix
// keeps them from colliding with other resources in the project.
func importName(image *oxidev1alpha1.Image) string {
	name := image.Spec.OxideName(image)
	if len(name) > 47 { // Oxide names are at most 63 characters.
		name = name[:47]
	}
	return name + "-import-" + string(image.UID)[:8]
}

// deleteImport deletes the temporary snapshot and disk of an image imported from a url.
func deleteImport(ctx context.Context, oc *oxide.Client, image *oxidev1alpha1.Image) error {
	if image.Spec.URL == "" {
		return nil
	}
	project, name := oxide.NameOrId(image.Spec.ProjectName()), oxide.NameOrId(importName(image))
	err := oc.SnapshotDelete(ctx, oxide.SnapshotDeleteParams{Project: project, Snapshot: name})
	if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
		return err
	}
	return deleteImportDisk(ctx, oc, project, name)
}

// deleteImportDisk stops and finalizes an import disk so it can be deleted, then deletes it.
// Stopping and finalizing fail harmlessly once the disk is past them; DiskDelete reports anything else.
func deleteImportDisk(ctx context.Context, oc *oxide.Client, project, disk oxide.NameOrId) error {
	_ = oc.DiskBulkWriteImportStop(ctx, oxide.DiskBulkWriteImportStopParams{Project: project, Disk: disk})
	_ = oc.DiskFinalizeImport(ctx, oxide.DiskFinalizeImportParams{Project: project, Disk: disk, Body: &oxide.FinalizeDisk{}})
	err := oc.DiskDelete(ctx, oxide.DiskDeleteParams{Project: project, Disk: disk})
	if errors.Is(err, oxide.ErrObjectNotFound) {
		return nil
	}
	return err
}

// viewImage looks up an image by name or ID in the project, falling back to silo images.
func viewImage(ctx context.Context, oc *oxide.Client, project, image string) (*oxide.Image, error) {
	img, err := oc.ImageView(ctx, oxide.ImageViewParams{Project: oxide.NameOrId(project), Image: oxide.NameOrId(image)})
	if errors.Is(err, oxide.ErrObjectNotFound) || errors.Is(err, oxide.ErrHTTP400) {
		// Not a project image by name: try silo images, or an image ID without a project.
		img, err = oc.ImageView(ctx, oxide.ImageViewParams{Image: oxide.NameOrId(image)})
	}
	return img, err
}

func (r *ImageReconciler) handleFinalizer(ctx context.Context, image *oxidev1alpha1.Image) error {
	if controllerutil.AddFinalizer(image, finalizerName) {
		return r.Update(ctx, image)
	}
	return nil
}

// handleDelete deletes the Oxide image and any leftover import, unless protected, and removes the finalizer.
func (r *ImageReconciler) handleDelete(ctx context.Context, image *oxidev1alpha1.Image) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(image, finalizerName) {
		return ctrl.Result{}, nil
	}
	if !image.Spec.DeletionProtection {
		oc, err := oxideclient.NewClientFromRef(ctx, r.Client, image.Spec.ConnectionRef.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := deleteImport(ctx, oc, image); err != nil {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		cur, err := viewImage(ctx, oc, image.Spec.ProjectName(), image.Spec.OxideName(image))
		if err == nil {
			err = oc.ImageDelete(ctx, oxide.ImageDeleteParams{Image: oxide.NameOrId(cur.Id)})
		}
		if err != nil && !errors.Is(err, oxide.ErrObjectNotFound) {
			return ctrl.Result{}, oxideclient.ShortError(err)
		}
		log.FromContext(ctx).Info("Deleted Oxide image")
	}
	controllerutil.RemoveFinalizer(image, finalizerName)
	return ctrl.Result{}, client.IgnoreNotFound(r.Update(ctx, image))
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oxidev1alpha1.Image{}, builder.WithPredicates(changed)).
		Named("oxide-image").
		Complete(r)
}
