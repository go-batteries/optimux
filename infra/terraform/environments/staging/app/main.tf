provider "aws" {
  region = var.AWS_REGION
}

module "root" {
  source = "../../../modules/app"

  AWS_REGION            = var.AWS_REGION
  ENVIRONMENT           = var.ENVIRONMENT
  S3_BASE_URL           = var.S3_BASE_URL
  BASTION_APP           = var.BASTION_APP
  AWS_ACCOUNT           = var.AWS_ACCOUNT
  MEDIA_SECURITY_GROUP  = var.MEDIA_SECURITY_GROUP
  VPC_NAME              = var.VPC_NAME
  DOMAIN_NAME           = var.DOMAIN_NAME
  CORS_ORIGINS          = var.CORS_ORIGINS
  APP_NAME              = var.APP_NAME
  SERVICE_NAME          = var.SERVICE_NAME
  APP_PORT              = var.APP_PORT
  APP_VERSION           = var.APP_VERSION
  DOCKER_IMAGE          = var.DOCKER_IMAGE
  AMI_IMAGE_ID          = var.AMI_IMAGE_ID
  INSTANCE_TYPE         = var.INSTANCE_TYPE
  COMMIT_VERSION        = var.COMMIT_VERSION
  QSIZE                 = var.QSIZE
  CLEANER_SIDECAR_IMAGE = var.CLEANER_SIDECAR_IMAGE
  FULL_DOMAIN_NAME      = var.FULL_DOMAIN_NAME
  APP_ENVIRONMENT       = var.APP_ENVIRONMENT
  PG_URL                = var.PG_URL
  DEFAULT_VPC_SG_ID     = var.DEFAULT_VPC_SG_ID
  DD_API_KEY            = var.DD_API_KEY
  DATADOG_SITE          = var.DATADOG_SITE
  DD_SERVICE            = var.DD_SERVICE
  DD_APP_NAME           = var.DD_APP_NAME
  LB_SUFFIX                = var.LB_SUFFIX
  STATSD_ADDR              = var.STATSD_ADDR
  ECS_DISCOVERY_TAG        = var.ECS_DISCOVERY_TAG
  ECS_SERVICE_MIN_CAPACITY  = var.ECS_SERVICE_MIN_CAPACITY
  ECS_SERVICE_MAX_CAPACITY  = var.ECS_SERVICE_MAX_CAPACITY
  ECS_SERVICE_DESIRED_COUNT = var.ECS_SERVICE_DESIRED_COUNT
  FFMPEG_HWACCEL            = var.FFMPEG_HWACCEL
  FFMPEG_THREADS            = var.FFMPEG_THREADS
}
