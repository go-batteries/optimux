# Elastic Transcoder module for video processing
# This module creates the necessary resources for AWS Elastic Transcoder

locals {
  derived_input_bucket_arn  = "arn:aws:s3:::${var.input_bucket_name}"
  derived_output_bucket_arn = "arn:aws:s3:::${var.output_bucket_name}"
}

# S3 buckets for input and output (if they don't exist)
resource "aws_s3_bucket" "input_bucket" {
  count = var.create_src_bucket ? 1 : 0

  bucket = var.input_bucket_name

  tags = {
    Name        = "${var.project_name}-video-input-${var.environment}"
    Environment = var.environment
    Purpose     = "ElasticTranscoder Input"
    Terraform   = true
  }
}

resource "aws_s3_bucket" "output_bucket" {
  count = var.create_dst_bucket ? 1 : 0

  bucket = var.output_bucket_name

  tags = {
    Name        = "${var.project_name}-video-output-${var.environment}"
    Environment = var.environment
    Purpose     = "ElasticTranscoder Output"
  }
}

# S3 bucket versioning
resource "aws_s3_bucket_versioning" "input_bucket_versioning" {
  count = var.create_src_bucket ? 1 : 0

  bucket = aws_s3_bucket.input_bucket[count.index].id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_versioning" "output_bucket_versioning" {
  count = var.create_dst_bucket ? 1 : 0

  bucket = aws_s3_bucket.output_bucket[count.index].id

  versioning_configuration {
    status = "Enabled"
  }
}


# S3 to SNS setup

# SNS Topic: to receive notifications when new videos are uploaded
resource "aws_sns_topic" "video_upload_topic" {
  name = "redernet-video-upload-topic-${var.environment}"
}

# SNS Topic Policy: allow S3 bucket to publish messages to this topic
resource "aws_sns_topic_policy" "video_upload_policy" {
  arn = aws_sns_topic.video_upload_topic.arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "s3.amazonaws.com"
        }
        Action   = "SNS:Publish"
        Resource = aws_sns_topic.video_upload_topic.arn
        Condition = {
          ArnLike = {
            "aws:SourceArn" = local.derived_input_bucket_arn
          }
        }
      }
    ]
  })
}

# S3 Bucket Notification: trigger SNS when a new object is created
resource "aws_s3_bucket_notification" "video_upload_notification" {
  bucket = var.input_bucket_name

  topic {
    topic_arn     = aws_sns_topic.video_upload_topic.arn
    events        = ["s3:ObjectCreated:*"]
    filter_prefix = var.video_bucket_prefix
  }

  depends_on = [aws_sns_topic_policy.video_upload_policy]
}

