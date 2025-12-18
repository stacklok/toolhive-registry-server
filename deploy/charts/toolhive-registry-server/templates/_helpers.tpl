{{/*
Expand the name of the chart.
*/}}
{{- define "toolhive-registry-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "toolhive-registry-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "toolhive-registry-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "toolhive-registry-server.labels" -}}
helm.sh/chart: {{ include "toolhive-registry-server.chart" . }}
{{ include "toolhive-registry-server.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "toolhive-registry-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "toolhive-registry-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: registry-api
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "toolhive-registry-server.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "toolhive-registry-server.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the configmap for the registry server config
*/}}
{{- define "toolhive-registry-server.configMapName" -}}
{{- printf "%s-config" (include "toolhive-registry-server.fullname" .) }}
{{- end }}

{{/*
Config file hash annotation for rolling updates
*/}}
{{- define "toolhive-registry-server.configHash" -}}
{{- .Values.config | toYaml | sha256sum | trunc 63 }}
{{- end }}
