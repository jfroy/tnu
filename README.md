# Talos Node Updater (tnu)

This is a Go program designed to run on a Talos node. It will determine if the node requires an
update based on the node's current Talos version and schematic, the desired version (passed as an
argument), and the schematic embedded in the node MachineConfig's install image URL. If an update
is required, it will issue an upgrade API call to the node.

## Requirements

Talos Node Updater will only work on nodes that have an Image Factory install image in their machine
config (see [`Config.machine.install`](https://www.talos.dev/v1.9/reference/configuration/v1alpha1/config/#Config.machine.install), [Boot Assets](https://www.talos.dev/v1.9/talos-guides/install/boot-assets/#example-bare-metal-with-image-factory),
and [Image Factory](https://www.talos.dev/v1.9/learn-more/image-factory/)).

## System Upgrade Controller

Talos Node Updater is easy to integrate with Rancher's
[System Upgrade Controller](https://github.com/rancher/system-upgrade-controller). Below is an
example plan:

```yaml
---
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
      - key: kubernetes.io/os
        operator: Exists
  tolerations:
    - key: node-role.kubernetes.io/control-plane
      operator: Exists
      effect: NoSchedule
  upgrade:
    image: ghcr.io/jfroy/tnu:latest
    envs:
      - name: NODE_IP
        valueFrom:
          fieldRef:
            fieldPath: status.hostIP
    args:
      - --node=$(NODE_IP)
      - --tag=$(SYSTEM_UPGRADE_PLAN_LATEST_VERSION)
```

If the cluster has [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery)
installed, then a more specific `matchExpressions` can be used:

```yaml
    matchExpressions:
      - key: feature.node.kubernetes.io/system-os_release.ID
        operator: In
        values: ["talos"]
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

### Force plan execution

To force a plan execution, delete the `plan.upgrade.cattle.io/<plan name>` node label. This is
necessary when using the example plan above after changing the install image in the machine config
(for example to update the node's install schematic).
