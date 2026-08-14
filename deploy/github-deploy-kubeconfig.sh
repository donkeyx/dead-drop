#!/usr/bin/env bash
# Build a kubeconfig for the dead-drop GitHub deploy ServiceAccount.
# Writes to ~/.secure/dead-drop-github-deploy.kubeconfig (not printed).
set -euo pipefail

ns=dead-drop
sa=dead-drop-github-deploy
secret=dead-drop-github-deploy-token
out="${DEAD_DROP_KUBECONFIG_OUT:-${HOME}/.secure/dead-drop-github-deploy.kubeconfig}"

mkdir -p "$(dirname "${out}")"

server="$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
if [ -z "${server}" ]; then
  echo "could not read current cluster server" >&2
  exit 1
fi

# Wait until the controller mints the token.
token=""
for _ in $(seq 1 20); do
  token="$(kubectl -n "${ns}" get secret "${secret}" -o jsonpath='{.data.token}' 2>/dev/null || true)"
  if [ -n "${token}" ]; then
    break
  fi
  sleep 1
done
if [ -z "${token}" ]; then
  echo "token not ready on secret/${secret}" >&2
  exit 1
fi

ca="$(kubectl -n "${ns}" get secret "${secret}" -o jsonpath='{.data.ca\.crt}')"
token_plain="$(printf '%s' "${token}" | base64 -d)"

umask 077
cat > "${out}" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: dead-drop
    cluster:
      server: ${server}
      certificate-authority-data: ${ca}
contexts:
  - name: dead-drop-github-deploy
    context:
      cluster: dead-drop
      namespace: ${ns}
      user: ${sa}
current-context: dead-drop-github-deploy
users:
  - name: ${sa}
    user:
      token: ${token_plain}
EOF

echo "wrote ${out}"
echo "Paste that file into GitHub → Settings → Environments → production → secret KUBECONFIG"
echo "Do not commit it."
