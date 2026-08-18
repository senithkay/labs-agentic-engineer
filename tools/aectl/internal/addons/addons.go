// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package addons declares optional platform resource types that can be
// installed after `aectl platform install`. Each Addon bundles one or more
// Kubernetes manifests applied via server-side apply. To add a new addon,
// append an entry to Available and embed its manifest(s) as string literals.
package addons

// Addon describes an optional platform resource type.
type Addon struct {
	ID          string
	Label       string
	Description string
	// Manifests is a list of YAML strings applied in order via server-side apply.
	// Each string may contain multiple documents separated by ---.
	Manifests []string
}

// Available is the ordered list of optional addons shown to the operator after
// platform install. Add new entries here to surface them in the interactive
// selector.
var Available = []Addon{
	{
		ID:          "postgres-cnpg",
		Label:       "postgres-cnpg",
		Description: "PostgreSQL via CloudNativePG (ClusterResourceType + RBAC)",
		Manifests:   []string{postgresCNPGResourceType, postgresCNPGRBAC},
	},
}

// postgresCNPGResourceType is the ClusterResourceType that makes postgres-cnpg
// available as a platform-resource dependency type in the AEP console.
// Source: deployments/single-cluster/resource-types/postgres-cnpg/resourcetype.yaml
const postgresCNPGResourceType = `
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterResourceType
metadata:
  name: postgres-cnpg
  annotations:
    aep.wso2.com/description: >-
      A dedicated PostgreSQL database cluster provisioned inside the platform
      (CloudNativePG). Declare on the service that owns the data.
spec:
  retainPolicy: Delete
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        version:
          type: string
          default: "16"
          enum: ["16", "15"]
        storage:
          type: string
          default: "1Gi"
          enum: ["1Gi", "5Gi", "10Gi"]
        instances:
          type: integer
          default: 1
          minimum: 1
          maximum: 3
  resources:
    - id: cluster
      template:
        apiVersion: postgresql.cnpg.io/v1
        kind: Cluster
        metadata:
          name: ${metadata.name}
          namespace: ${metadata.namespace}
          labels: ${metadata.labels}
        spec:
          instances: ${parameters.instances}
          imageName: ghcr.io/cloudnative-pg/postgresql:${parameters.version}
          storage:
            size: ${parameters.storage}
          bootstrap:
            initdb:
              database: appdb
              owner: appuser
  outputs:
    - name: host
      value: ${metadata.name}-rw.${metadata.namespace}.svc.cluster.local
    - name: port
      value: "5432"
    - name: dbname
      value: appdb
    - name: user
      secretKeyRef:
        name: ${metadata.name}-app
        key: username
    - name: password
      secretKeyRef:
        name: ${metadata.name}-app
        key: password
`

// postgresCNPGRBAC grants the OpenChoreo data-plane agent permission to manage
// CloudNativePG Cluster objects in project namespaces.
// Source: deployments/single-cluster/resource-types/postgres-cnpg/rbac.yaml
const postgresCNPGRBAC = `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: openchoreo-dataplane-cnpg
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
rules:
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters"]
    verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: openchoreo-dataplane-cnpg
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: openchoreo-dataplane-cnpg
subjects:
  - kind: ServiceAccount
    name: cluster-agent-dataplane
    namespace: openchoreo-data-plane
`
