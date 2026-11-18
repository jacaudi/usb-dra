{{/*
The whole values document for the bjw-s common library, built fresh. User values are never
merged into it: only the lookups below read them, so privileged, root and the host mounts
cannot be reached from values. common.yaml parses it with fromYaml.

values.yaml is empty, so every default lives in this file. A value that is set is used as
given, even when it is empty or zero: --exclude-classes= (publish everything but hubs),
-v=0, tolerations: [] and resources: {} are all meaningful. Hence hasKey and dig, not default.
*/}}
{{- define "usb-dra.values" -}}
{{- $image := .Values.image | default dict -}}
{{- $driver := .Values.driver | default dict -}}
# common >= 5.3.0 renders a Namespace from .Values.namespace; this document bypasses the
# library defaults, so seed it. Helm owns the release namespace.
namespace:
  create: false
# values.yaml is empty, so .Values.global can be absent; common needs a map there.
global:
  {{- toYaml (.Values.global | default dict) | nindent 2 }}

# The DaemonSet. Everything the deployment gotchas depend on is fixed here and is not
# reachable from values: root, privileged, the token mount, and no host namespaces.
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

# Host mounts. A hostPath item mounts at its own path, so the cdi volume and mount move
# together with the "usb-dra.cdiRoot" helper, which is also passed as --cdi-root.
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
    hostPath: {{ include "usb-dra.cdiRoot" . | quote }}
    hostPathType: DirectoryOrCreate
  sys:
    type: hostPath
    hostPath: /sys
    hostPathType: Directory
    globalMounts:
      - readOnly: true

# The service account the DaemonSet and the ClusterRoleBinding share.
serviceAccount:
  main:
    forceRename: {{ include "usb-dra.name" . }}

# The RBAC the driver needs: write its own ResourceSlices, read ResourceClaims, get its Node.
rbac:
  roles:
    main:
      type: ClusterRole
      forceRename: {{ include "usb-dra.name" . }}
      rules:
        # The resourceslice controller publishes and prunes this node's pool.
        - apiGroups:
            - resource.k8s.io
          resources:
            - resourceslices
          verbs:
            - get
            - list
            - watch
            - create
            - update
            - patch
            - delete
        # The kubeletplugin helper fetches the claim itself before calling
        # NodePrepareResources; without this, prepare fails with a forbidden error and
        # the pod stays in ContainerCreating.
        - apiGroups:
            - resource.k8s.io
          resources:
            - resourceclaims
          verbs:
            - get
            - list
            - watch
        # Needed to resolve the node UID used as the owner reference on slices, so they
        # are garbage-collected when the node goes away.
        - apiGroups:
            - ""
          resources:
            - nodes
          verbs:
            - get
  bindings:
    main:
      type: ClusterRoleBinding
      forceRename: {{ include "usb-dra.name" . }}
      roleRef:
        identifier: main
      subjects:
        - identifier: main
{{- end -}}
