# gitlab-enhanced — Azure deployment
#
# Provisions a GitLab Omnibus instance on an Azure VM with:
#   - Resource group, VNet, subnet, NSG
#   - Linux VM (Ubuntu 24.04) with GitLab via custom_data
#   - Azure Blob Storage account for object storage (LFS, artefacts, registry)
#   - Public IP + DNS label (optional Azure DNS zone record)
#   - Managed identity for VM → Storage access (no static credentials)
#
# Usage:
#   cd deploy/terraform/azure
#   terraform init
#   terraform apply \
#     -var="gitlab_domain=gitlab.example.com" \
#     -var="ssh_public_key=$(cat ~/.ssh/id_ed25519.pub)"

terraform {
  required_version = ">= 1.6"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "azurerm" {
  features {}
}

# ── Variables ─────────────────────────────────────────────────────────────────

variable "location" {
  description = "Azure region"
  type        = string
  default     = "eastus"
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

variable "vm_size" {
  description = "Azure VM size"
  type        = string
  default     = "Standard_D4s_v3"
}

variable "os_disk_size_gb" {
  description = "OS disk size in GB"
  type        = number
  default     = 100
}

variable "ssh_public_key" {
  description = "SSH public key for VM access"
  type        = string
}

variable "allowed_source_address" {
  description = "Source IP/CIDR allowed to reach GitLab"
  type        = string
  default     = "*"
}

variable "dns_zone_name" {
  description = "Azure DNS zone name (leave empty to skip DNS record)"
  type        = string
  default     = ""
}

variable "dns_zone_resource_group" {
  description = "Resource group containing the DNS zone (required if dns_zone_name is set)"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags applied to all resources"
  type        = map(string)
  default     = {project = "gitlab-enhanced"}
}

# ── Random suffix for globally unique names ───────────────────────────────────

resource "random_string" "suffix" {
  length  = 6
  upper   = false
  special = false
}

# ── Resource group ────────────────────────────────────────────────────────────

resource "azurerm_resource_group" "main" {
  name     = "gitlab-enhanced"
  location = var.location
  tags     = var.tags
}

# ── Networking ────────────────────────────────────────────────────────────────

resource "azurerm_virtual_network" "main" {
  name                = "gitlab-enhanced-vnet"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags
}

resource "azurerm_subnet" "main" {
  name                 = "gitlab-enhanced-subnet"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_network_security_group" "gitlab" {
  name                = "gitlab-enhanced-nsg"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags

  security_rule {
    name                       = "HTTPS"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "443"
    source_address_prefix      = var.allowed_source_address
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "HTTP"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = var.allowed_source_address
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "SSH"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.allowed_source_address
    destination_address_prefix = "*"
  }
}

resource "azurerm_subnet_network_security_group_association" "main" {
  subnet_id                 = azurerm_subnet.main.id
  network_security_group_id = azurerm_network_security_group.gitlab.id
}

resource "azurerm_public_ip" "gitlab" {
  name                = "gitlab-enhanced-ip"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = var.tags
}

resource "azurerm_network_interface" "gitlab" {
  name                = "gitlab-enhanced-nic"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.main.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.gitlab.id
  }
}

# ── Storage account ───────────────────────────────────────────────────────────

resource "azurerm_storage_account" "gitlab" {
  name                     = "gitlabenhanced${random_string.suffix.result}"
  resource_group_name      = azurerm_resource_group.main.name
  location                 = azurerm_resource_group.main.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  min_tls_version          = "TLS1_2"
  tags                     = var.tags
}

resource "azurerm_storage_container" "gitlab" {
  name                  = "gitlab-objects"
  storage_account_name  = azurerm_storage_account.gitlab.name
  container_access_type = "private"
}

# ── Managed identity ──────────────────────────────────────────────────────────

resource "azurerm_user_assigned_identity" "gitlab" {
  name                = "gitlab-enhanced"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags
}

resource "azurerm_role_assignment" "gitlab_storage" {
  scope                = azurerm_storage_account.gitlab.id
  role_definition_name = "Storage Blob Data Contributor"
  principal_id         = azurerm_user_assigned_identity.gitlab.principal_id
}

# ── VM ────────────────────────────────────────────────────────────────────────

resource "azurerm_linux_virtual_machine" "gitlab" {
  name                = "gitlab-enhanced"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  size                = var.vm_size
  admin_username      = "ubuntu"
  tags                = var.tags

  network_interface_ids = [azurerm_network_interface.gitlab.id]

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.gitlab.id]
  }

  admin_ssh_key {
    username   = "ubuntu"
    public_key = var.ssh_public_key
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Premium_LRS"
    disk_size_gb         = var.os_disk_size_gb
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }

  custom_data = base64encode(templatefile("${path.module}/cloud_init.sh.tpl", {
    gitlab_domain        = var.gitlab_domain
    gitlab_edition       = var.gitlab_edition
    storage_account_name = azurerm_storage_account.gitlab.name
    storage_container    = azurerm_storage_container.gitlab.name
  }))

  lifecycle {
    ignore_changes = [custom_data]
  }
}

# ── DNS (optional) ────────────────────────────────────────────────────────────

resource "azurerm_dns_a_record" "gitlab" {
  count               = var.dns_zone_name != "" ? 1 : 0
  name                = "@"
  zone_name           = var.dns_zone_name
  resource_group_name = var.dns_zone_resource_group
  ttl                 = 300
  records             = [azurerm_public_ip.gitlab.ip_address]
  tags                = var.tags
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "gitlab_ip" {
  description = "Public IP of the GitLab VM"
  value       = azurerm_public_ip.gitlab.ip_address
}

output "gitlab_url" {
  description = "GitLab URL"
  value       = "https://${var.gitlab_domain}"
}

output "storage_account" {
  description = "Azure Storage account name for GitLab object storage"
  value       = azurerm_storage_account.gitlab.name
}

output "storage_container" {
  description = "Azure Blob container name"
  value       = azurerm_storage_container.gitlab.name
}

output "ssh_command" {
  description = "SSH command to connect to the VM"
  value       = "ssh ubuntu@${azurerm_public_ip.gitlab.ip_address}"
}
