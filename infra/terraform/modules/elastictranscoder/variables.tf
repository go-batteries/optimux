variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "environment" {
  description = "Environment name (e.g., dev, staging, prod)"
  type        = string
  default     = "dev"
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "optimux"
}

variable "input_bucket_name" {
  description = "Name of the S3 bucket for input videos"
  type        = string
}

variable "output_bucket_name" {
  description = "Name of the S3 bucket for output videos"
  type        = string
}

variable "create_src_bucket" {
  type    = bool
  default = false
}

variable "create_dst_bucket" {
  type    = bool
  default = false
}

variable "video_bucket_prefix" {
  type        = string
  description = "s3 path prefix for videos"
}
