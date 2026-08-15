# dead-drop Helm chart

This chart deploys stateless `dead-drop` replicas backed by an external PostgreSQL database.

Published artifacts (from `v*` tags only):

| Kind | Location |
|------|----------|
| Images | `ghcr.io/donkeyx/dead-drop` and `docker.io/donkeyx/dead-drop` (`X.Y.Z`, `X.Y`, `latest`) |
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

Ingress is disabled by default and the base values contain no hostname. Set the ingress class, host paths, annotations, and TLS secret for the target cluster in the overlay. This is a Helm 3 chart; do not use the obsolete `helm init` command.

Install the published chart:

```bash
helm upgrade --install dead-drop oci://ghcr.io/donkeyx/charts/dead-drop \
  --version 0.1.3 \
  -n dead-drop --create-namespace \
  -f deploy/helm/dead-drop/values.local.yaml
```

Or the chart from this repo:

```bash
helm upgrade --install dead-drop ./deploy/helm/dead-drop \
  -n dead-drop --create-namespace \
  -f deploy/helm/dead-drop/values.local.yaml
```

To pull the image from Docker Hub instead of GHCR, set this in the overlay:

```yaml
image:
  repository: docker.io/donkeyx/dead-drop
  tag: "0.1.3"
```

If the GHCR packages are private, add a pull secret and `--set imagePullSecrets[0].name=ghcr-pull-secret`.

## GitHub Actions deploy

Image publish stays on the `ci` environment. Cluster changes use a separate **`production`** environment. Both live in [`.github/workflows/release.yml`](../../../.github/workflows/release.yml).

A `v*` tag on `master` publishes the multi-arch image and OCI chart, then deploys. **Run workflow** redeploys an existing semver (`0.1.3` / `v0.1.3`) without rebuilding. Master and PRs only run CI — no image, no deploy. The job checks out `v<version>` so the chart matches the image.

Create the `production` environment on the repo, then add:

| Kind | Name | Value |
|------|------|--------|
| Secret | `KUBECONFIG` | Deploy-only kubeconfig YAML (not a laptop admin file) |
| Secret | `HELM_VALUES` | Cluster overlay YAML — same shape as `values.example.yaml`. `image.tag` is set by the workflow. |
| Variable | `SMOKE_URL` | Public origin, e.g. `https://drop.donkeyx.dev`. After rollout, Playwright creates, opens, and burns a dummy drop. |
| Secret | `SMOKE_BYPASS` | Same value as the Cloudflare WAF skip header `x-dead-drop-smoke`. |

Ready is the chart's `/readyz` probe plus `helm --wait` and `kubectl rollout status`. A second job then runs the live Playwright smoke in `mcr.microsoft.com/playwright:v1.62.1-noble` (Chromium already in the image). On Helm failure the deploy job dumps events and logs; on smoke failure it uploads a Playwright trace.

Mint the kubeconfig once (needs cluster-admin). It can only change objects in `dead-drop`:

```bash
kubectl apply -f deploy/github-deploy-rbac.yaml
./deploy/github-deploy-kubeconfig.sh
# paste ~/.secure/dead-drop-github-deploy.kubeconfig into KUBECONFIG
```

`ci` Hub credentials must not be copied here. The DB URL, origin TLS, and Grafana OTLP token stay as cluster Secrets; they are not in GitHub.

## Metrics and Grafana Cloud

`/metrics` listens on container port 9090 (`DEADDROP_METRICS_ADDR`). Ingress only routes the public HTTP port, so Prometheus text is cluster-internal. To push metrics and traces to Grafana Cloud, create a Secret and set `grafana.existingSecret` in the overlay:

```bash
kubectl -n dead-drop create secret generic dead-drop-grafana \
  --from-literal=otlp-endpoint='https://otlp-gateway-prod-REGION.grafana.net/otlp' \
  --from-literal=otlp-headers='Authorization=Basic BASE64(instance-id:api-token)'
```

```yaml
grafana:
  existingSecret: dead-drop-grafana
```

Empty `grafana.existingSecret` leaves `/metrics` on and does not export OTLP. Never label metrics or spans with a secret id.

Set `autoscaling.enabled=true` to enable HPA. PostgreSQL is required for multiple replicas; SQLite and filesystem storage are not supported by this chart. The application rate limiter remains per pod until a shared limiter is added.

A CronJob (`expire.enabled`, default every 5 minutes) runs `dead-drop expire` against the same database so TTL-elapsed drops are deleted even if nobody Takes them. The GitHub deploy Role must include `batch` CronJobs (see `deploy/github-deploy-rbac.yaml`).

The chart includes a `/startupz` probe with a two-and-a-half-minute failure budget. The server opens the configured store and completes database migrations before it starts listening; Kubernetes waits for this probe before evaluating readiness and liveness. Startup phases are logged without logging database URLs or secret data. Override `startupProbe` in the values overlay if the target cluster needs different timing. Default resource requests are `10m` CPU and `50Mi` memory, with limits of `500m` CPU and `512Mi` memory; these defaults are based on observed steady-state usage. If enabling CPU-based HPA, revisit the target because a low CPU request makes utilization percentages sensitive to small bursts.
