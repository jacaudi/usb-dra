{{/*
Host mounts. A hostPath item mounts at its own path, so the cdi volume and mount move
together with driver.cdiRoot, which is also passed as --cdi-root.
*/}}
{{- define "usb-dra.values.persistence" -}}
persistence:
  plugins-registry:
    type: hostPath
    hostPath: /var/lib/kubelet/plugins_registry
    hostPathType: Directory
  plugins:
    type: hostPath
    hostPath: /var/lib/kubelet/plugins
    hostPathType: Directory
  cdi:
    type: hostPath
    hostPath: {{ .Values.driver.cdiRoot }}
    hostPathType: DirectoryOrCreate
  sys:
    type: hostPath
    hostPath: /sys
    hostPathType: Directory
    globalMounts:
      - readOnly: true
{{- end -}}
