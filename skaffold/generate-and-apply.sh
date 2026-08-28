#!/bin/sh
# Applies platform namespaces and secrets, then generates any one-time secrets
# that cannot be committed to source control.
# Idempotent: safe to re-run.
set -e

NAMESPACE=wso2-aep

# Bootstrap per-developer helm-values.yaml from the example if not present.
# Developers should set their own smee.io channel in this file.
if [ ! -f skaffold/helm-values.yaml ]; then
  cp skaffold/helm-values.yaml.example skaffold/helm-values.yaml
  echo "Created skaffold/helm-values.yaml from example — set your smee.io channel in it"
fi

kubectl apply -f skaffold/namespace.yaml
kubectl apply -f skaffold/addon-namespaces.yaml
kubectl apply -f skaffold/secrets.yaml

# Generate the RSA task-signing key on first install only.
# Rotating it invalidates all in-flight task JWTs, so skip if it already exists.
if kubectl get secret aep-task-signing-key -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "aep-task-signing-key already exists — skipping"
else
  echo "Generating aep-task-signing-key..."
  RSA_KEY=$(openssl genrsa 2048 2>/dev/null)
  kubectl create secret generic aep-task-signing-key \
    --namespace="$NAMESPACE" \
    --from-literal="task-signing.pem=$RSA_KEY"
  echo "aep-task-signing-key created"
fi
