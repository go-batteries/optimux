provider "aws" {
  region = var.aws_region
}

module "root" {
  source = "../../../modules/elastictranscoder"

  environment        = var.environment
  project_name       = var.project_name
  input_bucket_name  = var.input_bucket_name
  output_bucket_name = var.output_bucket_name

  create_src_bucket   = var.create_src_bucket
  create_dst_bucket   = var.create_dst_bucket
  video_bucket_prefix = var.video_bucket_prefix
}
