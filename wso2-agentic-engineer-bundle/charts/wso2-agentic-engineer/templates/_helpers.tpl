{{/*
Chart name
*/}}
{{- define "asdlc.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "asdlc.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "asdlc.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "asdlc.selectorLabels" -}}
app.kubernetes.io/name: {{ include "asdlc.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name
*/}}
{{- define "asdlc.serviceAccountName" -}}
{{- print "asdlc-api" }}
{{- end }}

{{/*
Main app secret name — holds auto-generated + user-supplied secrets
*/}}
{{- define "asdlc.appSecretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- print "asdlc-app" }}
{{- end }}
{{- end }}

{{/*
Task signing key secret name
*/}}
{{- define "asdlc.taskSigningKeySecretName" -}}
{{- if .Values.taskSigning.existingSecret }}
{{- .Values.taskSigning.existingSecret }}
{{- else }}
{{- print "asdlc-task-signing-key" }}
{{- end }}
{{- end }}

{{/*
PostgreSQL secret name
*/}}
{{- define "asdlc.postgresSecretName" -}}
{{- if .Values.postgres.auth.existingSecret }}
{{- .Values.postgres.auth.existingSecret }}
{{- else }}
{{- print "asdlc-postgres" }}
{{- end }}
{{- end }}

{{/*
PostgreSQL host
*/}}
{{- define "asdlc.postgresHost" -}}
{{- if .Values.postgres.enabled }}
{{- print "asdlc-postgres" }}
{{- else }}
{{- .Values.postgres.external.host }}
{{- end }}
{{- end }}
