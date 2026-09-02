#!/bin/sh
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Applies platform namespaces and secrets, then generates any one-time secrets
# that cannot be committed to source control.
# Idempotent: safe to re-run. Skips on subsequent Skaffold dev cycles once a
# sentinel ConfigMap is written to the cluster — deleted automatically when the
# cluster is reset.
set -e

NAMESPACE=wso2-aep
SENTINEL=aep-generate-setup-done

if kubectl get configmap "$SENTINEL" -n "$NAMESPACE" >/dev/null 2>&1; then
  echo "Secrets already seeded — skipping (delete configmap/$SENTINEL to force re-run)"
  exit 0
fi

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

kubectl create configmap "$SENTINEL" -n "$NAMESPACE" --from-literal=done=true 2>/dev/null || true
