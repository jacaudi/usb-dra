{{/*
The service account the DaemonSet and the ClusterRoleBinding share.
*/}}
{{- define "usb-dra.values.serviceaccount" -}}
serviceAccount:
  main:
    forceRename: {{ include "usb-dra.name" . }}
{{- end -}}
