{{/*
Expand the name of the chart.
*/}}
{{- define "mockagents.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name. Truncated at 63 chars to
satisfy DNS label limits.
*/}}
{{- define "mockagents.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name and version label.
*/}}
{{- define "mockagents.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels applied to every object created by the chart.
*/}}
{{- define "mockagents.labels" -}}
helm.sh/chart: {{ include "mockagents.chart" . }}
{{ include "mockagents.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels — a minimal subset that matches Pods. Must not change
across upgrades (Deployment selector is immutable in Kubernetes).
*/}}
{{- define "mockagents.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mockagents.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Name of the ServiceAccount to use.
*/}}
{{- define "mockagents.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "mockagents.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Resolve the image ref: repository:tag where tag defaults to Chart.AppVersion.
*/}}
{{- define "mockagents.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Name of the agents ConfigMap. Prefers an existing CM when set.
*/}}
{{- define "mockagents.agentsConfigMapName" -}}
{{- if .Values.agents.existingConfigMap -}}
{{- .Values.agents.existingConfigMap -}}
{{- else -}}
{{- printf "%s-agents" (include "mockagents.fullname" .) -}}
{{- end -}}
{{- end -}}
