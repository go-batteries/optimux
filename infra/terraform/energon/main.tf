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

data "aws_s3_bucket" "selected" {
  bucket = var.S3_BUCKET_NAME
}

data "aws_vpc" "selected" {
  tags = {
    Name = var.VPC_NAME
  }
}

data "aws_subnets" "private_subnets" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.selected.id]
  }

  tags = {
    Private     = "true"
    Environment = var.ENVIRONMENT
  }
}

# SQS Queue for S3 Upload Events
resource "aws_sqs_queue" "s3_event_queue" {
  name                       = "${var.APP_NAME}-s3-event-queue"
  message_retention_seconds  = 300
  visibility_timeout_seconds = 840 # Lambda Max Timeout 900s (15m)

  tags = {
    Environment = var.ENVIRONMENT
    Service     = var.APP_NAME
    Terraform   = true
  }
}

# SQS Queue Policy to allow S3 to send messages
resource "aws_sqs_queue_policy" "s3_event_queue_policy" {
  queue_url = aws_sqs_queue.s3_event_queue.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = "*"
        Action    = "sqs:SendMessage"
        Resource  = aws_sqs_queue.s3_event_queue.arn
        Condition = {
          ArnLike = {
            "aws:SourceArn" = "arn:aws:s3:::${var.S3_BUCKET_NAME}"
          }
        }
      },
      {
        Effect = "Allow",
        Principal = {
          Service = "s3.amazonaws.com"
        },
        Action   = "lambda:InvokeFunction",
        Resource = aws_lambda_function.s3_event_lambda.arn,
        Condition = {
          ArnLike = {
            "aws:SourceArn" : "arn:aws:s3:::${var.S3_BUCKET_NAME}"
          }
        }
      }
    ]
  })
}

# Attach S3 Event Notification to the existing bucket
resource "aws_s3_bucket_notification" "s3_event_notification" {
  count = var.ENVIRONMENT == "staging" ? 1 : 0

  bucket = data.aws_s3_bucket.selected.id

  dynamic "queue" {
    for_each = var.S3_FILTER_PREFIXES

    content {
      queue_arn     = aws_sqs_queue.s3_event_queue.arn
      events        = ["s3:ObjectCreated:*"]
      filter_prefix = queue.value
    }
  }
}

# IAM Role for Lambda Execution
resource "aws_iam_role" "lambda_role" {
  name = "${var.APP_NAME}-lambda-s3-sqs-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })
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
        Action   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"]
        Resource = aws_sqs_queue.s3_event_queue.arn
      },
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "arn:aws:logs:*:*:*"
      },
      {
        Effect = "Allow",
        Action = [
          "ec2:CreateNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DeleteNetworkInterface"
        ],
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:ListBucket",
          "s3:PutObject",
          "s3:PutObjectTagging",
        ]
        Resource = [
          "arn:aws:s3:::${var.S3_BUCKET_NAME}",
          "arn:aws:s3:::${var.S3_BUCKET_NAME}/*"
        ]
      }
    ]
  })
}

# Attach Policy to Lambda Role
resource "aws_iam_role_policy_attachment" "lambda_policy_attachment" {
  role       = aws_iam_role.lambda_role.name
  policy_arn = aws_iam_policy.lambda_policy.arn
}


resource "aws_security_group" "lambda_sg" {
  name   = "${var.APP_NAME}-lambda-sg"
  vpc_id = data.aws_vpc.selected.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Environment = var.ENVIRONMENT
    Terraform   = "true"
    Name        = var.APP_NAME
  }
}

resource "aws_security_group_rule" "allow_lambda_to_rds" {
  description              = "Energon Lambda Worker DB RW access"
  type                     = "ingress"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = var.RDS_SG_ID
  source_security_group_id = aws_security_group.lambda_sg.id
}

# Lambda Function
resource "aws_lambda_function" "s3_event_lambda" {
  function_name = "${var.APP_NAME}-s3-sqs-processor"
  role          = aws_iam_role.lambda_role.arn
  architectures = ["x86_64"]

  package_type = "Image"
  image_uri    = "${var.AWS_ACCOUNT}.dkr.ecr.us-east-1.amazonaws.com/energon-worker:${var.APP_VERSION}"

  publish = true

  timeout = 720 # 12 minutes

  vpc_config {
    subnet_ids         = data.aws_subnets.private_subnets.ids
    security_group_ids = [aws_security_group.lambda_sg.id]
  }

  ephemeral_storage {
    size = 1024 # 1024 MB
  }

  environment {
    variables = {
      SQS_QUEUE_URL = aws_sqs_queue.s3_event_queue.id
      READ_PG_DBURL = var.READ_PG_DBURL
      ENVIRONMENT   = var.ENVIRONMENT
      PG_DBURL      = var.PG_DBURL
      APP           = "energon-worker"
      DD_SERVICE    = "energon-worker"
      type          = "lambda"
    }
  }

  tags = {
    Environment = var.ENVIRONMENT
    Terraform   = "true"
    App         = var.APP_NAME
  }
}

# Lambda Trigger from SQS
resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn                   = aws_sqs_queue.s3_event_queue.arn
  function_name                      = aws_lambda_function.s3_event_lambda.arn
  batch_size                         = 10
  maximum_batching_window_in_seconds = 30
}

# Lambda Concurrency Control
resource "aws_lambda_function_url" "s3_event_lambda_url" {
  function_name      = aws_lambda_function.s3_event_lambda.function_name
  authorization_type = "NONE"
}

resource "aws_lambda_provisioned_concurrency_config" "lambda_concurrency" {
  function_name                     = aws_lambda_function.s3_event_lambda.function_name
  qualifier                         = aws_lambda_function.s3_event_lambda.version
  provisioned_concurrent_executions = 2
}
