# dead-drop Helm chart

This chart deploys stateless `dead-drop` replicas backed by an external PostgreSQL database.

Published artifacts (from `master` and `v*` tags):

| Kind | Location |
|------|----------|
| Image | `ghcr.io/donkeyx/dead-drop` (`latest`, `sha-<git>`, semver on tags) |
| Chart | `oci://ghcr.io/donkeyx/charts/dead-drop` |

Create the namespace (optional if you pass `--create-namespace`) and the database URL Secret separately. The chart does not create either.

```bash
kubectl create secret generic dead-drop-db \
  -n dead-drop \
  --from-literal=database-url='postgres://deaddrop:password@postgres.example/deaddrop?sslmode=require'
```

Cluster-specific image tag, database secret name, and ingress live in a values overlay. Copy the example (gitignored as `values.local.yaml`):

```bash
cp deploy/helm/dead-drop/values.example.yaml deploy/helm/dead-drop/values.local.yaml
# edit host, tls secret name, image.tag
```

Install the published chart:

```bash
helm upgrade --install dead-drop oci://ghcr.io/donkeyx/charts/dead-drop \
  --version 0.1.0 \
  -n dead-drop --create-namespace \
  -f deploy/helm/dead-drop/values.local.yaml
```

Or the chart from this repo:

```bash
helm upgrade --install dead-drop ./deploy/helm/dead-drop \
  -n dead-drop --create-namespace \
  -f deploy/helm/dead-drop/values.local.yaml
```

If the GHCR packages are private, add a pull secret and `--set imagePullSecrets[0].name=ghcr-pull-secret`.

Set `autoscaling.enabled=true` to enable HPA. PostgreSQL is required for multiple replicas; SQLite and filesystem storage are not supported by this chart. The application rate limiter remains per pod until a shared limiter is added.
