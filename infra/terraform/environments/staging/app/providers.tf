terraform {
  backend "s3" {
    bucket         = "terraform-state-optimux"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "terraform-locks"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0" # Or use "= 5.32.0" for an exact version
    }
  }
}
