# runtime/k8s-in-incus

Provisions a Kubernetes cluster inside Incus VMs on a single bare-metal host.
Ported from [Esteban-Cruz/K8s-in-incus](https://github.com/Esteban-Cruz/K8s-in-incus)
(MIT) to Ansible roles for integration with the gitlab-enhanced deployment stack.

## Prerequisites

- Incus 6.0+ installed and initialised (`incus admin init`)
- Nested virtualisation enabled (`kvm-ok`)
- Ansible 2.15+ with `ansible.utils` collection
- `yq` 4.x (for Gitpod playbook)

```bash
ansible-galaxy collection install ansible.utils
```

## Playbooks

| Playbook | What it does |
|---|---|
| `k8s-cluster.yml` | Provisions control plane + N worker VMs, initialises K8s |
| `k8s-addons.yml` | Installs local-path-provisioner (StorageClass) + ingress-nginx |
| `k8s-gitpod.yml` | Installs Gitpod Classic on the cluster |
| `k8s-gitlab-runner.yml` | Installs GitLab Runner (Kubernetes executor) |
| `k8s-cleanup.yml` | Destroys all VMs and network |

## Quick Start

```bash
cd runtime/k8s-in-incus/ansible

# 1. Provision a 1 control-plane + 1 worker cluster (default)
ansible-playbook playbooks/k8s-cluster.yml

# 2. Install cluster add-ons (StorageClass + ingress)
ansible-playbook playbooks/k8s-addons.yml

# 3. Use the cluster
export KUBECONFIG=kubeconfig.yaml
kubectl get nodes
kubectl get storageclass          # local-path (default)
kubectl get pods -n ingress-nginx # ingress-nginx-controller

# 4. Install Gitpod Classic (optional — requires add-ons)
ansible-playbook playbooks/k8s-gitpod.yml \
  -e gitpod_domain=gitpod.gitlab.local

# 5. Install GitLab Runner (optional)
ansible-playbook playbooks/k8s-gitlab-runner.yml \
  -e gitlab_url=https://gitlab.local \
  -e gitlab_runner_token=glrt-xxxxxxxxxxxx

# 6. Tear down
ansible-playbook playbooks/k8s-cleanup.yml
```

## Scaling

```bash
# 3-worker cluster with more resources
ansible-playbook playbooks/k8s-cluster.yml \
  -e k8s_worker_count=3 \
  -e incus_vm_cpus=4 \
  -e incus_vm_memory=8GiB
```

## Role Reference

| Role | Purpose |
|---|---|
| `incus_network` | Creates the Incus bridge network |
| `incus_vm` | Launches an Incus VM with static IP |
| `containerd` | Installs containerd inside a VM |
| `kubernetes_node` | Installs kubeadm/kubelet/kubectl inside a VM |
| `kubeadm_init` | Initialises the control plane, fetches kubeconfig |
| `kubeadm_join` | Joins a worker node to the cluster |
| `local_path_provisioner` | Installs Rancher local-path-provisioner as default StorageClass |
| `nginx_ingress` | Installs ingress-nginx with fixed NodePorts (no cloud LB required) |

## Architecture

```
Bare metal host
└── Incus
    ├── control-plane (VM, 10.100.0.2)
    │   ├── kube-apiserver
    │   ├── kube-controller-manager
    │   ├── kube-scheduler
    │   ├── etcd
    │   └── [Gitpod meta components, if installed]
    └── worker-1 (VM, 10.100.0.3)
        ├── kubelet
        ├── containerd
        └── [Gitpod workspace pods / GitLab Runner pods]
```

## Differences from Original Shell Scripts

| Original | This port |
|---|---|
| Hard-coded Incus 6.0.0 version check | Removed — any 6.x works |
| Hard-coded yq 4.2.0 version check | Removed — any 4.x works |
| Single control plane only | Multi-worker via `k8s_worker_count` |
| No persistent storage | Add-on: local-path-provisioner (`k8s-addons.yml`) |
| No ingress | Add-on: nginx-ingress (`k8s-addons.yml`) |
| Shell scripts | Ansible roles — integrates with GET |
