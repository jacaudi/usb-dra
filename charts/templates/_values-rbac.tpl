{{/*
The RBAC the driver needs: write its own ResourceSlices, read ResourceClaims, get its Node.
*/}}
{{- define "usb-dra.values.rbac" -}}
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
