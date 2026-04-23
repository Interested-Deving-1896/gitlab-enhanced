# K8s-in-Incus CI Actions

GitLab CI job templates for managing the Kubernetes-in-Incus cluster lifecycle
from CI pipelines. Include these templates in your `.gitlab-ci.yml` to automate
cluster provisioning, Gitpod deployment, and teardown.

## Available templates

### `cluster.yml` — Cluster lifecycle jobs

```yaml
include:
  - local: runtime/k8s-in-incus/actions/cluster.yml

# Trigger cluster provisioning
provision-k8s:
  extends: .k8s-incus-provision
  rules:
    - if: $PROVISION_K8S == "1"

# Tear down the cluster
destroy-k8s:
  extends: .k8s-incus-destroy
  rules:
    - if: $DESTROY_K8S == "1"
      when: manual
```

### `gitpod.yml` — Gitpod Classic deployment jobs

```yaml
include:
  - local: runtime/k8s-in-incus/actions/gitpod.yml

deploy-gitpod:
  extends: .gitpod-deploy
  rules:
    - if: $DEPLOY_GITPOD == "1"
```

## Variables

| Variable              | Default              | Description                          |
|-----------------------|----------------------|--------------------------------------|
| `K8S_INCUS_HOST`      | `localhost`          | Host running Incus                   |
| `K8S_INCUS_SOCKET`    | `/var/lib/incus/unix.socket` | Incus socket path           |
| `K8S_NODE_COUNT`      | `3`                  | Number of K8s worker nodes           |
| `K8S_CONTROL_COUNT`   | `1`                  | Number of control plane nodes        |
| `GITPOD_VERSION`      | `latest`             | Gitpod Classic version to deploy     |
| `GITLAB_URL`          | (required)           | GitLab instance URL for Gitpod OAuth |
