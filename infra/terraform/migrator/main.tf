terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}

provider "aws" {
  region = var.AWS_REGION
}

data "terraform_remote_state" "app_infrastructure" {
  backend = "s3"
  config = {
    bucket = var.TF_S3_BUCKET_NAME
    key    = var.TF_S3_KEY
    region = var.AWS_REGION
  }
}

# IAM Policy for Lambda to read SQS and write logs
resource "aws_iam_policy" "lambda_policy" {
  name        = "${var.APP_NAME}-lambda-sqs-policy"
  description = "Policy for Lambda to process SQS messages and log to CloudWatch"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "arn:aws:logs:*:*:*"
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:CreateNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DeleteNetworkInterface",
          "ec2:DescribeSecurityGroups",
          "ec2:DescribeSubnets",
          "ec2:DescribeVpcs"
        ]
        Resource = "*"
      }
    ]
  })
}

# Attach Policy to Lambda Role
resource "aws_iam_role_policy_attachment" "lambda_policy_attachment" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = aws_iam_policy.lambda_policy.arn
}

# Lambda Function
# s3_event_lambda
resource "aws_lambda_function" "migration_lambda" {
  function_name = "${var.APP_NAME}-migrate"
  role          = aws_iam_role.lambda_role.arn
  architectures = ["x86_64"]

  package_type = "Image"
  image_uri    = "${var.AWS_ACCOUNT}.dkr.ecr.us-east-1.amazonaws.com/${var.APP_NAME}:${var.APP_VERSION}"

  publish = true

  timeout = 300 # 5 minutes

  vpc_config {
    subnet_ids         = data.terraform_remote_state.app_infrastructure.outputs.private_subnet_ids
    security_group_ids = data.terraform_remote_state.app_infrastructure.outputs.alb_security_group_id
  }

  image_config {
    command = [var.MIGRATION_ACTION]
  }

  environment {
    variables = {
      PG_URL    = var.PG_URL
      APP       = var.APP_NAME
      type      = "lambda"
      Terraform = "true"
    }
  }
}

