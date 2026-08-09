### Setting up shared file system

resource "aws_efs_file_system" "app_logs" {
  creation_token = "${var.APP_NAME}-app-logs"
  tags = {
    Name        = "${var.APP_NAME}-app-logs"
    Environment = var.ENVIRONMENT
    Terraform   = true
  }
}

resource "aws_efs_file_system" "image_cache" {
  creation_token = "${var.APP_NAME}-image-cache"
  tags = {
    Name        = "${var.APP_NAME}-image-cache"
    Environment = var.ENVIRONMENT
    Terraform   = true
  }
}


