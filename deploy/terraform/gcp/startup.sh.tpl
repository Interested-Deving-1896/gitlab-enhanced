#!/usr/bin/env bash
# GCP startup script: installs GitLab Omnibus and configures GCS object storage.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq curl ca-certificates

curl -fsSL https://packages.gitlab.com/install/repositories/gitlab/gitlab-${gitlab_edition}/script.deb.sh | bash
EXTERNAL_URL="https://${gitlab_domain}" apt-get install -y gitlab-${gitlab_edition}

cat >> /etc/gitlab/gitlab.rb <<'EOF'
# Object storage — uses GCE service account, no static credentials
gitlab_rails['object_store']['enabled'] = true
gitlab_rails['object_store']['provider'] = 'Google'
gitlab_rails['object_store']['google_project'] = '${gcp_project}'
gitlab_rails['object_store']['google_json_key_location'] = ''  # use metadata server
gitlab_rails['object_store']['connection'] = {
  'provider' => 'Google',
  'google_project' => '${gcp_project}'
}
gitlab_rails['object_store']['objects']['artifacts']['bucket'] = '${gcs_bucket}'
gitlab_rails['object_store']['objects']['lfs']['bucket'] = '${gcs_bucket}'
gitlab_rails['object_store']['objects']['uploads']['bucket'] = '${gcs_bucket}'
gitlab_rails['object_store']['objects']['packages']['bucket'] = '${gcs_bucket}'
EOF

gitlab-ctl reconfigure
