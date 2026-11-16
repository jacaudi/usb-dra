{{/*
The DaemonSet. Everything the deployment gotchas depend on is fixed here and is not
reachable from values: root, privileged, the token mount, and no host namespaces.
*/}}
{{- define "usb-dra.values.controllers" -}}
controllers:
  main:
    type: daemonset
    forceRename: {{ include "usb-dra.name" . }}
    serviceAccount:
      identifier: main
    pod:
      automountServiceAccountToken: true
      enableServiceLinks: false
      hostIPC: false
      hostNetwork: false
      hostPID: false
      priorityClassName: {{ .Values.priorityClassName }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
    containers:
      plugin:
        image:
          repository: {{ .Values.image.repository }}
          tag: {{ include "usb-dra.imageTag" . | quote }}
          pullPolicy: {{ .Values.image.pullPolicy }}
        args:
          - --driver-name=usb.jacaudi.dev
          - {{ printf "--rescan-interval=%s" .Values.driver.rescanInterval | quote }}
          - {{ printf "--exclude-classes=%s" .Values.driver.excludeClasses | quote }}
          {{- range .Values.driver.include }}
          - {{ printf "--include=%s" . | quote }}
          {{- end }}
          - {{ printf "--cdi-root=%s" .Values.driver.cdiRoot | quote }}
          - {{ printf "-v=%d" (.Values.driver.verbosity | int) | quote }}
        env:
          NODE_NAME:
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
        securityContext:
          # Must be root: the plugin socket under /var/lib/kubelet/plugins and the CDI
          # specs under the CDI root are root-owned on the host. privileged alone does
          # not change the UID.
          privileged: true
          runAsUser: 0
        resources:
          {{- toYaml .Values.resources | nindent 10 }}
        # The driver serves no health endpoint; common would otherwise add TCP probes.
        probes:
          liveness:
            enabled: false
          readiness:
            enabled: false
          startup:
            enabled: false
{{- end -}}
