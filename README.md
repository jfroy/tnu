# Talos Node Updater (tnu)

This is a Go program designed to run on a Talos node. It will determine if the node requires an
upgrade based on the node's current Talos version and schematic, the desired version (passed as an
argument), and the schematic embedded in the node MachineConfig's install image URL. If an upgrade
is required, it will issue an upgrade API call to the node.

Talos Node Updater is easy to integrate with Rancher's
[System Upgrade Controller](https://github.com/rancher/system-upgrade-controller). Below is an
example plan that will work with any Talos node:

```yaml
---
# yaml-language-server: $schema=https://kubernetes-schemas.pages.dev/upgrade.cattle.io/plan_v1.json
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: talos
spec:
  version: x.y.z
  serviceAccountName: system-upgrade
  secrets:
    - name: talos
      path: /var/run/secrets/talos.dev
      ignoreUpdates: true
  concurrency: 1
  exclusive: true
  nodeSelector:
    matchExpressions:
      - key: feature.node.kubernetes.io/system-os_release.ID
        operator: In
        values: ["talos"]
  tolerations:
    - key: node-role.kubernetes.io/control-plane
      operator: Exists
      effect: NoSchedule
  upgrade:
    image: ghcr.io/jfroy/tnu:latest
    envs:
      - name: NODE
        valueFrom:
          fieldRef:
            fieldPath: spec.nodeName
    args:
      - --node=$(NODE)
      - --tag=$(SYSTEM_UPGRADE_PLAN_LATEST_VERSION)
      # - --powercycle # Optional, Talos reboots via the kexec syscall by default
```

Talos Node Updater needs a service account that can access the Talos API and read `Node` resources.
The following RBAC resources should work, but see the
[Talos documentation](https://www.talos.dev/v1.9/advanced/talos-api-access-from-k8s/) for more
details.

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system-upgrade
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: system-upgrade
    namespace: talos-admin
---
apiVersion: talos.dev/v1alpha1
kind: ServiceAccount
metadata:
  name: talos
spec:
  roles:
    - os:admin
```
