{{/*
The one name every object the chart creates shares: the release name. It is passed to
common as forceRename for the service account, ClusterRole, ClusterRoleBinding and
DaemonSet, so the admission policy and the binding subject can never drift from the
service account the pod runs as. The schema rejects every override of it.
*/}}
{{- define "usb-dra.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Image tag: an explicit image.tag verbatim, else "v" + appVersion with any leading "v"
stripped, matching the v-prefixed tags ci-build pushes.
*/}}
{{- define "usb-dra.imageTag" -}}
{{- .Values.image.tag | default (printf "v%s" (.Chart.AppVersion | trimPrefix "v")) -}}
{{- end -}}
