variable "APP_NAME" {
  type = string
}

variable "S3_BUCKET_NAME" {
  type = string
}

variable "S3_FILTER_PREFIXES" {
  type = list(string)
}

variable "ENVIRONMENT" {
  type = string
}

variable "AWS_REGION" {
  type    = string
  default = "us-east-1"
}
