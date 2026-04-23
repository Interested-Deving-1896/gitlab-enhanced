#!/usr/bin/env bash
# EC2 user_data: installs GitLab Omnibus and configures S3 object storage.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq curl ca-certificates

# Install GitLab
curl -fsSL https://packages.gitlab.com/install/repositories/gitlab/gitlab-${gitlab_edition}/script.deb.sh | bash
EXTERNAL_URL="https://${gitlab_domain}" apt-get install -y gitlab-${gitlab_edition}

# Configure S3 object storage
cat >> /etc/gitlab/gitlab.rb <<'EOF'
# Object storage — uses EC2 instance role, no static credentials
gitlab_rails['object_store']['enabled'] = true
gitlab_rails['object_store']['provider'] = 'AWS'
gitlab_rails['object_store']['region'] = '${aws_region}'
gitlab_rails['object_store']['bucket'] = '${s3_bucket}'
gitlab_rails['object_store']['connection'] = {
  'provider' => 'AWS',
  'region' => '${aws_region}',
  'use_iam_profile' => true
}
gitlab_rails['object_store']['objects']['artifacts']['bucket'] = '${s3_bucket}'
gitlab_rails['object_store']['objects']['lfs']['bucket'] = '${s3_bucket}'
gitlab_rails['object_store']['objects']['uploads']['bucket'] = '${s3_bucket}'
gitlab_rails['object_store']['objects']['packages']['bucket'] = '${s3_bucket}'
EOF

gitlab-ctl reconfigure
