{{/*
Expand the name of the chart.
*/}}
{{- define "cloudgate.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "cloudgate.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s" $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "cloudgate.labels" -}}
helm.sh/chart: {{ include "cloudgate.name" . }}-{{ .Chart.Version }}
{{ include "cloudgate.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cloudgate.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cloudgate.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
