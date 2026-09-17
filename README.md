# oxide-operator

A Kubernetes operator for [Oxide](https://oxide.computer) racks. Declare projects, VPCs, disks, snapshots,
images and instances as Kubernetes resources and the operator keeps them in sync with the Oxide API.

## Resources

All resources live in `oxide.100vms.com/v1alpha1`.

| Kind | Scope | Oxide resource |
|------|-------|----------------|
| `OxideConnection` | Cluster | Silo endpoint + API token (from a Secret) |
| `Project` | Namespaced | Project |
| `Vpc` | Namespaced | VPC and, optionally, its firewall rules |
| `VpcSubnet` | Namespaced | VPC subnet |
| `Disk` | Namespaced | Disk (blank, from image or from snapshot) |
| `Snapshot` | Namespaced | Disk snapshot |
| `Image` | Namespaced | Image, imported from a raw image URL or created from a snapshot, optionally promoted to the silo |
| `Instance` | Namespaced | Instance, with an optional boot disk created alongside it |
| `InstanceSet` | Namespaced | N identical `Instance`s named `<set>-0 … <set>-N` |

The Kubernetes object name is the Oxide resource name, so it must be a valid Oxide name
(lowercase letters, digits and `-`, starting with a letter, at most 63 characters).

## Install

```sh
helm install oxide-operator oci://ghcr.io/alperencelik/charts/oxide-operator \
  --namespace oxide-operator-system --create-namespace
```

Or apply the manifest attached to a [release](https://github.com/alperencelik/oxide-operator/releases):

```sh
kubectl apply -f https://github.com/alperencelik/oxide-operator/releases/latest/download/install.yaml
```

## Quick start

See [docs/quick-start.md](docs/quick-start.md).

## Development

```sh
make lint          # golangci-lint
make install run   # install CRDs and run the manager against the current kubeconfig
```

Deploy to a cluster with `make docker-build docker-push deploy IMG=<registry>/oxide-operator:tag`.

## Disclaimer

Oxide, [Omicron](https://github.com/oxidecomputer/omicron) and [Helios](https://github.com/oxidecomputer/helios) are
developed and published by [Oxide Computer Company](https://oxide.computer). Omicron and Helios are licensed under the
[Mozilla Public License 2.0](https://mozilla.org/MPL/2.0/).

This is an independent personal project and is not affiliated with, endorsed by, or supported by Oxide Computer
Company.

## License

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
