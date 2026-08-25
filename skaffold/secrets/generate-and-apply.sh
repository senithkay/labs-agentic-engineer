#!/bin/sh
# Generates platform secrets and applies them to the cluster.
# Idempotent: re-running does not rotate already-created keys.
set -e

NAMESPACE=wso2-aep

# Apply the namespace manifest first so the ConfigMap and RSA key secret have
# somewhere to land. Skaffold will apply this again during its manifest deploy — idempotent.
kubectl apply -f skaffold/namespace.yaml

# --- Config import (equivalent to: aectl platform config import --config skaffold/configs.yaml) ---
# Reads the nested YAML config, flattens to dot-notation keys, and writes them to
# the aep-cli-config ConfigMap. Uses the same key list as config.ConfigMapKeys in aectl.
echo "Importing config from skaffold/configs.yaml..."
python3 - <<'PYEOF' | kubectl apply -f -
import sys, yaml

with open('skaffold/configs.yaml') as f:
    cfg = yaml.safe_load(f)

def flatten(obj, prefix=''):
    out = {}
    for k, v in (obj or {}).items():
        key = '{}.{}'.format(prefix, k) if prefix else k
        if isinstance(v, dict):
            out.update(flatten(v, key))
        elif v is None:
            out[key] = ''
        elif isinstance(v, bool):
            out[key] = 'true' if v else 'false'
        else:
            out[key] = str(v)
    return out

KNOWN_KEYS = [
    'thunder.namespace', 'thunder.url', 'thunder.config_map', 'thunder.deployment',
    'thunder.admin_client_id', 'thunder.public_url',
    'oc.api_url', 'oc.system_namespace', 'oc.org_namespace', 'oc.local_org_provisioning.enabled',
    'platform.workspaces.access_mode',
    'codingagent.openbao_direct.enabled',
    'openbao.addr',
    'webhook.delivery_url', 'webhook.local_smee.enabled',
    'gateway.hostname',
]

flat = flatten(cfg)
data = {k: flat.get(k, '') for k in KNOWN_KEYS}

cm = {
    'apiVersion': 'v1',
    'kind': 'ConfigMap',
    'metadata': {
        'name': 'aep-cli-config',
        'namespace': 'wso2-aep',
        'labels': {'app.kubernetes.io/managed-by': 'skaffold'},
    },
    'data': data,
}
sys.stdout.write(yaml.dump(cm, default_flow_style=False))
PYEOF

# Generate the RSA task-signing key only on first install.
# Rotating it would invalidate all in-flight task JWTs, so we skip if the secret
# already exists.
if kubectl get secret aep-task-signing-key -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "aep-task-signing-key already exists — skipping generation"
else
  echo "Generating RSA task-signing key..."
  RSA_KEY=$(openssl genrsa 2048 2>/dev/null)
  kubectl create secret generic aep-task-signing-key \
    --namespace="$NAMESPACE" \
    --from-literal="task-signing.pem=$RSA_KEY"
  echo "aep-task-signing-key created"
fi
