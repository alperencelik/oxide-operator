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

// Package oxideclient builds Oxide API clients from OxideConnections and watches instances for drift.
package oxideclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oxidev1alpha1 "github.com/alperencelik/oxide-operator/api/oxide/v1alpha1"
)

// insecureHTTPClient is shared by all connections with insecureSkipVerify so they reuse one connection
// pool; oxide.WithInsecureSkipVerify would clone a new transport per client. Other clients use the
// SDK default, which already shares http.DefaultTransport.
var insecureHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Only used for connections that opt in with spec.insecureSkipVerify.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: transport, Timeout: 600 * time.Second}
}()

var (
	clientsMu sync.Mutex
	clients   = map[string]connectionClient{}
)

// connectionClient is the client built for an OxideConnection, tagged with the UID, generation and token Secret
// resourceVersion it was built from.
type connectionClient struct {
	spec   string
	client *oxide.Client
}

// NewClientFromRef returns an Oxide client for the named OxideConnection. The token Secret is read when the
// connection is first used and again when its spec or status.observedSecretResourceVersion changes; the
// OxideConnection controller watches the Secret and bumps that status field when the token rotates.
//
// c is normally the manager's client, which serves OxideConnections from its cache and reads Secrets
// straight from the API server (Secrets are excluded from the cache in cmd/main.go).
func NewClientFromRef(ctx context.Context, c client.Reader, name string) (*oxide.Client, error) {
	conn := &oxidev1alpha1.OxideConnection{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, conn); err != nil {
		return nil, fmt.Errorf("getting OxideConnection %q: %w", name, err)
	}
	return ClientFor(ctx, c, conn)
}

// ClientFor is NewClientFromRef for an OxideConnection that has already been fetched.
func ClientFor(ctx context.Context, c client.Reader, conn *oxidev1alpha1.OxideConnection) (*oxide.Client, error) {
	spec := fmt.Sprintf("%s/%d/%s", conn.UID, conn.Generation, conn.Status.ObservedSecretResourceVersion)

	clientsMu.Lock()
	defer clientsMu.Unlock()
	if cached, ok := clients[conn.Name]; ok && cached.spec == spec {
		return cached.client, nil
	}
	oc, err := newClient(ctx, c, conn)
	if err != nil {
		return nil, err
	}
	clients[conn.Name] = connectionClient{spec: spec, client: oc}
	return oc, nil
}

// Returns a new Oxide client based on the OxideConnection.
func newClient(ctx context.Context, c client.Reader, conn *oxidev1alpha1.OxideConnection) (*oxide.Client, error) {
	ref := conn.Spec.TokenSecretRef
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return nil, fmt.Errorf("getting token Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	token := strings.TrimSpace(string(secret.Data[ref.Key]))
	if token == "" {
		return nil, fmt.Errorf("key %q of Secret %s/%s is empty", ref.Key, ref.Namespace, ref.Name)
	}
	opts := []oxide.ClientOption{
		oxide.WithHost(conn.Spec.Host),
		oxide.WithToken(token),
		oxide.WithUserAgent("oxide-operator"),
	}
	if conn.Spec.InsecureSkipVerify {
		opts = append(opts, oxide.WithHTTPClient(insecureHTTPClient))
	}
	return oxide.NewClient(opts...)
}
