terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

provider "aws" {
  region = var.AWS_REGION
}

resource "aws_s3_bucket" "selected" {
  bucket = var.S3_BUCKET_NAME

  tags = {
    Service     = var.APP_NAME
    Environment = var.ENVIRONMENT
    Terraform   = true
  }
}

resource "aws_s3_object" "folders" {
  for_each = toset(var.S3_FILTER_PREFIXES)

  bucket = aws_s3_bucket.selected.id
  key    = each.value
}
