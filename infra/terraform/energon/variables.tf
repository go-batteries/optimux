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

variable "APP_VERSION" {
  type = string
}

variable "AWS_ACCOUNT" {
  type        = string
  description = "aws account id"
}

variable "READ_PG_DBURL" {
  type        = string
  description = "postgres read db url"
}

variable "PG_DBURL" {
  type        = string
  description = "postgres write db url"
}

variable "VPC_NAME" {
  type = string
}

variable "RDS_SG_ID" {
  type = string
}
