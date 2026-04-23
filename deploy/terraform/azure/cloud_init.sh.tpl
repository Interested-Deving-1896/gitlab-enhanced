#!/usr/bin/env bash
# Azure cloud-init: installs GitLab Omnibus and configures Azure Blob object storage.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq curl ca-certificates

curl -fsSL https://packages.gitlab.com/install/repositories/gitlab/gitlab-${gitlab_edition}/script.deb.sh | bash
EXTERNAL_URL="https://${gitlab_domain}" apt-get install -y gitlab-${gitlab_edition}

cat >> /etc/gitlab/gitlab.rb <<'EOF'
# Object storage — uses managed identity via Azure Instance Metadata Service
gitlab_rails['object_store']['enabled'] = true
gitlab_rails['object_store']['provider'] = 'AzureRM'
gitlab_rails['object_store']['azure_storage_account_name'] = '${storage_account_name}'
gitlab_rails['object_store']['azure_storage_access_key'] = ''  # use managed identity
gitlab_rails['object_store']['connection'] = {
  'provider' => 'AzureRM',
  'azure_storage_account_name' => '${storage_account_name}',
  'azure_storage_access_key' => ''
}
gitlab_rails['object_store']['objects']['artifacts']['bucket'] = '${storage_container}'
gitlab_rails['object_store']['objects']['lfs']['bucket'] = '${storage_container}'
gitlab_rails['object_store']['objects']['uploads']['bucket'] = '${storage_container}'
gitlab_rails['object_store']['objects']['packages']['bucket'] = '${storage_container}'
EOF

gitlab-ctl reconfigure
