# gitlab-enhanced — AWS deployment
#
# Provisions a GitLab Omnibus instance on an EC2 VM with:
#   - VPC, subnet, security group
#   - EC2 instance (Ubuntu 24.04) with GitLab installed via user_data
#   - S3 bucket for object storage (LFS, artefacts, registry)
#   - Route53 A record (optional, requires hosted_zone_id)
#   - ACM certificate (optional, requires domain)
#
# Usage:
#   cd deploy/terraform/aws
#   terraform init
#   terraform apply \
#     -var="gitlab_domain=gitlab.example.com" \
#     -var="hosted_zone_id=Z1234567890"

terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ── Variables ─────────────────────────────────────────────────────────────────

variable "aws_region" {
  description = "AWS region to deploy into"
  type        = string
  default     = "us-east-1"
}

variable "gitlab_domain" {
  description = "External hostname for GitLab (e.g. gitlab.example.com)"
  type        = string
}

variable "gitlab_edition" {
  description = "GitLab edition: ce or ee"
  type        = string
  default     = "ce"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.xlarge"
}

variable "volume_size_gb" {
  description = "Root EBS volume size in GB"
  type        = number
  default     = 100
}

variable "hosted_zone_id" {
  description = "Route53 hosted zone ID (leave empty to skip DNS)"
  type        = string
  default     = ""
}

variable "ssh_public_key" {
  description = "SSH public key for EC2 access"
  type        = string
}

variable "allowed_cidr" {
  description = "CIDR allowed to reach GitLab (HTTPS + SSH)"
  type        = string
  default     = "0.0.0.0/0"
}

variable "tags" {
  description = "Tags applied to all resources"
  type        = map(string)
  default     = {project = "gitlab-enhanced"}
}

# ── Data ──────────────────────────────────────────────────────────────────────

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }
}

# ── Networking ────────────────────────────────────────────────────────────────

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  tags                 = merge(var.tags, {Name = "gitlab-enhanced"})
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = merge(var.tags, {Name = "gitlab-enhanced"})
}

resource "aws_subnet" "main" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true
  tags                    = merge(var.tags, {Name = "gitlab-enhanced"})
}

resource "aws_route_table" "main" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = merge(var.tags, {Name = "gitlab-enhanced"})
}

resource "aws_route_table_association" "main" {
  subnet_id      = aws_subnet.main.id
  route_table_id = aws_route_table.main.id
}

resource "aws_security_group" "gitlab" {
  name        = "gitlab-enhanced"
  description = "GitLab Omnibus"
  vpc_id      = aws_vpc.main.id

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [var.allowed_cidr]
  }

  ingress {
    description = "HTTP (redirect to HTTPS)"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [var.allowed_cidr]
  }

  ingress {
    description = "Git over SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.allowed_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {Name = "gitlab-enhanced"})
}

# ── SSH key ───────────────────────────────────────────────────────────────────

resource "aws_key_pair" "gitlab" {
  key_name   = "gitlab-enhanced"
  public_key = var.ssh_public_key
  tags       = var.tags
}

# ── EC2 instance ──────────────────────────────────────────────────────────────

resource "aws_instance" "gitlab" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.main.id
  vpc_security_group_ids = [aws_security_group.gitlab.id]
  key_name               = aws_key_pair.gitlab.key_name

  root_block_device {
    volume_size           = var.volume_size_gb
    volume_type           = "gp3"
    delete_on_termination = true
  }

  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    gitlab_domain  = var.gitlab_domain
    gitlab_edition = var.gitlab_edition
    s3_bucket      = aws_s3_bucket.gitlab.bucket
    aws_region     = var.aws_region
  })

  tags = merge(var.tags, {Name = "gitlab-enhanced"})

  lifecycle {
    ignore_changes = [ami]
  }
}

resource "aws_eip" "gitlab" {
  instance = aws_instance.gitlab.id
  domain   = "vpc"
  tags     = merge(var.tags, {Name = "gitlab-enhanced"})
}

# ── S3 object storage ─────────────────────────────────────────────────────────

resource "aws_s3_bucket" "gitlab" {
  bucket_prefix = "gitlab-enhanced-"
  tags          = var.tags
}

resource "aws_s3_bucket_versioning" "gitlab" {
  bucket = aws_s3_bucket.gitlab.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "gitlab" {
  bucket = aws_s3_bucket.gitlab.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "gitlab" {
  bucket                  = aws_s3_bucket.gitlab.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# IAM role for EC2 → S3 access (no static credentials needed)
resource "aws_iam_role" "gitlab" {
  name = "gitlab-enhanced-ec2"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = {Service = "ec2.amazonaws.com"}
      Action    = "sts:AssumeRole"
    }]
  })
  tags = var.tags
}

resource "aws_iam_role_policy" "gitlab_s3" {
  name = "gitlab-enhanced-s3"
  role = aws_iam_role.gitlab.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
      Resource = [
        aws_s3_bucket.gitlab.arn,
        "${aws_s3_bucket.gitlab.arn}/*"
      ]
    }]
  })
}

resource "aws_iam_instance_profile" "gitlab" {
  name = "gitlab-enhanced"
  role = aws_iam_role.gitlab.name
}

# ── DNS (optional) ────────────────────────────────────────────────────────────

resource "aws_route53_record" "gitlab" {
  count   = var.hosted_zone_id != "" ? 1 : 0
  zone_id = var.hosted_zone_id
  name    = var.gitlab_domain
  type    = "A"
  ttl     = 300
  records = [aws_eip.gitlab.public_ip]
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "gitlab_ip" {
  description = "Public IP of the GitLab instance"
  value       = aws_eip.gitlab.public_ip
}

output "gitlab_url" {
  description = "GitLab URL"
  value       = "https://${var.gitlab_domain}"
}

output "s3_bucket" {
  description = "S3 bucket name for GitLab object storage"
  value       = aws_s3_bucket.gitlab.bucket
}

output "ssh_command" {
  description = "SSH command to connect to the instance"
  value       = "ssh ubuntu@${aws_eip.gitlab.public_ip}"
}
