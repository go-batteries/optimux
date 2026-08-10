terraform {
  # Backend intentionally left partial: supply your own state bucket via
  # `terraform init -backend-config="bucket=<your-terraform-state-bucket>" \
  #   -backend-config="dynamodb_table=<your-lock-table>"`, or a gitignored
  # backend.hcl file. Do not hardcode a bucket name here.
  backend "s3" {
    region  = "us-east-1"
    encrypt = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0" # Or use "= 5.32.0" for an exact version
    }
  }
}
