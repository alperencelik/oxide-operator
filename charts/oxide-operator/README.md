# oxide-operator

A Kubernetes operator for [Oxide](https://oxide.computer) racks.

## Install

```sh
helm repo add alperencelik https://alperencelik.github.io/helm-charts
helm install oxide-operator alperencelik/oxide-operator \
  --namespace oxide-operator-system --create-namespace
```

Or from GHCR, where the same chart is pushed as an OCI artifact:

```sh
helm install oxide-operator oci://ghcr.io/alperencelik/charts/oxide-operator \
  --namespace oxide-operator-system --create-namespace
```

CRDs live in `crds/`, so Helm installs them but does not upgrade or delete them.
Apply `config/crd/bases` yourself when upgrading across a CRD change.

Then create a token Secret and an `OxideConnection`. See
[docs/quick-start.md](https://github.com/alperencelik/oxide-operator/blob/main/docs/quick-start.md).

## Values

| Key | Default | What it does |
|-----|---------|--------------|
| `replicaCount` | `1` | Manager replicas. Set `leaderElect` when more than one. |
| `image.repository` | `ghcr.io/alperencelik/oxide-operator` | Manager image. |
| `image.tag` | `""` | Defaults to the chart's `appVersion`. |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | |
| `serviceAccount.create` | `true` | |
| `serviceAccount.name` | `""` | Defaults to the release fullname. |
| `serviceAccount.annotations` | `{}` | |
| `leaderElect` | `true` | Passes `--leader-elect` and creates the lease Role. |
| `extraArgs` | `[]` | Extra manager flags, e.g. `["--instance-poll-interval=30s"]`. |
| `resources` | 500m/128Mi limits, 10m/64Mi requests | |
| `podAnnotations`, `podLabels` | `{}` | |
| `nodeSelector`, `tolerations`, `affinity` | empty | |

The manager reads Secrets cluster-wide (for `OxideConnection` tokens) and owns every
`oxide.100vms.com` resource. Those rules mirror `config/rbac/role.yaml`.
