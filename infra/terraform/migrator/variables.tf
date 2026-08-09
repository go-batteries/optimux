variable "APP_NAME" {
  type = string
}

variable "TF_S3_BUCKET_NAME" {
  type = string
}

variable "TF_S3_KEY" {
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

variable "AWS_ACCOUNT" {
  type        = string
  description = "aws account id"
}

variable "APP_VERSION" {
  type    = string
  default = "v24.7"
}

variable "PG_URL" {
  type = string
}

variable "MIGRATION_ACTION" {
  type    = string
  default = "up"
}
