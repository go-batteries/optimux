variable "ENVIRONMENT" {
  type    = string
  default = "dev"
}

variable "AWS_REGION" {
  type = string
}

variable "S3_BASE_URL" {
  type = string
}

variable "BASTION_APP" {
  type = string
}

variable "AWS_ACCOUNT" {
  type = string
}

variable "MEDIA_SECURITY_GROUP" {
  type    = string
  default = "null"
}

variable "VPC_NAME" {
  type = string
}

variable "DOMAIN_NAME" {
  type = string
}

variable "CORS_ORIGINS" {
  type        = string
  description = "allowed cors origins"
}

variable "APP_NAME" {
  type        = string
  description = "ecs app name. this will be prefixed for resouces like lb, key pair, ecs family"
}

variable "SERVICE_NAME" {
  type        = string
  description = "name of the project"
  default     = "optimux-server"
}

variable "APP_PORT" {
  type = number
}

variable "APP_VERSION" {
  type    = string
  default = "latest"
}

variable "DOCKER_IMAGE" {
  type = string
}

variable "AMI_IMAGE_ID" {
  type = string
}

variable "INSTANCE_TYPE" {
  type    = string
  default = "t2.xlarge"
}

variable "COMMIT_VERSION" {
  type = string
}

variable "QSIZE" {
  type    = number
  default = 32768
}

variable "CLEANER_SIDECAR_IMAGE" {
  type = string
}

variable "FULL_DOMAIN_NAME" {
  type = string
}

variable "APP_ENVIRONMENT" {
  type = string
}

variable "PG_URL" {
  type = string
}

variable "DEFAULT_VPC_SG_ID" {
  type = string
}

variable "DD_API_KEY" {
  type = string
}

variable "DATADOG_SITE" {
  type    = string
  default = "datadoghq.com"
}

variable "DD_SERVICE" {
  type    = string
  default = "optimux"
}

variable "DD_APP_NAME" {
  type        = string
  default     = "datadog-agent"
  description = "host name to be used for dd"
}

variable "LB_SUFFIX" {
  type = string
}

variable "STATSD_ADDR" {
  type    = string
  default = "datadog-agent:8125"
}


variable "ECS_DISCOVERY_TAG" {
  type    = string
  default = "v0.1.3"
}

variable "ECS_SERVICE_MIN_CAPACITY" {
  type        = number
  default     = 2
  description = "Minimum number of ECS tasks to run"
}

variable "ECS_SERVICE_MAX_CAPACITY" {
  type        = number
  default     = 10
  description = "Maximum number of ECS tasks to run"
}

variable "ECS_SERVICE_DESIRED_COUNT" {
  type        = number
  default     = 2
  description = "Desired number of ECS tasks to run"
}

variable "FFMPEG_HWACCEL" {
  type        = string
  default     = ""
  description = "FFmpeg hardware acceleration: vaapi (Linux/GPU), videotoolbox (macOS), or empty for none"
}

variable "FFMPEG_THREADS" {
  type        = number
  default     = 2
  description = "Number of threads per FFmpeg command (capped at 2 for AWS)"
}
