{{/*
The DaemonSet. Everything the deployment gotchas depend on is fixed here and is not
reachable from values: root, privileged, the token mount, and no host namespaces.

values.yaml is empty, so every default lives in this file. A value that is set is used as
given, even when it is empty or zero: --exclude-classes= (publish everything but hubs),
-v=0, tolerations: [] and resources: {} are all meaningful. Hence hasKey and dig, not default.
*/}}
{{- define "usb-dra.values.controllers" -}}
{{- $image := .Values.image | default dict -}}
{{- $driver := .Values.driver | default dict -}}
controllers:
  main:
    type: daemonset
    forceRename: {{ include "usb-dra.name" . | quote }}
    serviceAccount:
      identifier: main
    pod:
      automountServiceAccountToken: true
      enableServiceLinks: false
      hostIPC: false
      hostNetwork: false
      hostPID: false
      priorityClassName: {{ dig "priorityClassName" "system-node-critical" .Values.AsMap | quote }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- if not (hasKey .Values "tolerations") }}
      tolerations:
        # Every node: an empty pool means "healthy, no devices".
        - operator: Exists
      {{- else if .Values.tolerations }}
      tolerations:
        {{- toYaml .Values.tolerations | nindent 8 }}
      {{- end }}
    containers:
      plugin:
        image:
          repository: {{ $image.repository | default "ghcr.io/jacaudi/usb-dra" | quote }}
          tag: {{ include "usb-dra.imageTag" . | quote }}
          pullPolicy: {{ $image.pullPolicy | default "IfNotPresent" | quote }}
        args:
          - --driver-name=usb.jacaudi.dev
          {{- /* The binary's own defaults are the single source for these two. */}}
          {{- if hasKey $driver "rescanInterval" }}
          - {{ printf "--rescan-interval=%s" $driver.rescanInterval | quote }}
          {{- end }}
          {{- if hasKey $driver "excludeClasses" }}
          - {{ printf "--exclude-classes=%s" $driver.excludeClasses | quote }}
          {{- end }}
          {{- range $driver.include }}
          - {{ printf "--include=%s" . | quote }}
          {{- end }}
          - {{ printf "--cdi-root=%s" (include "usb-dra.cdiRoot" .) | quote }}
          - {{ printf "-v=%d" (dig "driver" "verbosity" 2 .Values.AsMap | int) | quote }}
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
          {{- if hasKey .Values "resources" }}
          {{- toYaml .Values.resources | nindent 10 }}
          {{- else }}
          requests:
            cpu: 10m
            memory: 32Mi
          limits:
            memory: 128Mi
          {{- end }}
        # The driver serves no health endpoint; common would otherwise add TCP probes.
        probes:
          liveness:
            enabled: false
          readiness:
            enabled: false
          startup:
            enabled: false
{{- end -}}
