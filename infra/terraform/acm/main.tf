variable "DOMAIN_NAME" {
  type = string
}

variable "AWS_REGION" {
  type    = string
  default = "us-east-1"
}

variable "APP_NAME" {
  type = string
}

variable "ENVIRONMENT" {
  type    = string
  default = "prod"
}

provider "aws" {
  region = var.AWS_REGION
}


resource "aws_acm_certificate" "my_cert" {
  domain_name       = var.DOMAIN_NAME
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name        = "${var.APP_NAME}-acm-cert"
    Environment = var.ENVIRONMENT
  }
}

output "dns_validation" {
  value = aws_acm_certificate.my_cert.domain_validation_options
}
