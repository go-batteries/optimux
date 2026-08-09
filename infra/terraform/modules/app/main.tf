data "aws_iam_policy_document" "assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type = "Service"
      identifiers = [
        "ec2.amazonaws.com",
        "ecs.amazonaws.com",
        "ecs-tasks.amazonaws.com",
      ]
    }
  }
}

data "aws_iam_policy_document" "ecs_agent" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type = "Service"
      identifiers = [
        "ecs-tasks.amazonaws.com",
        "ecs.amazonaws.com",
        "ec2.amazonaws.com"
      ]
    }
  }
}

data "aws_iam_policy_document" "ssmGetParamRule" {
  statement {
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters"
    ]

    resources = ["*"]
  }
}

data "aws_iam_policy_document" "ecsCloudWatchLogsPolicy" {
  statement {
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:ListTagsForResource",
      "elasticloadbalancing:RegisterTargets",
      "elasticloadbalancing:DeregisterTargets",
      "elasticloadbalancing:DescribeTargetHealth"
    ]

    resources = ["arn:aws:logs:${var.AWS_REGION}:${var.AWS_ACCOUNT}:log-group:/ecs/*:*"]
  }
}

data "aws_secretsmanager_secret" "dockerhub" {
  name = "docker-credentials"
}

data "aws_vpc" "selected" {
  tags = {
    Name = var.VPC_NAME
  }
}

data "aws_vpc" "default" {
  default = true
}

data "aws_instance" "bastion_host" {
  filter {
    name   = "tag:Name"
    values = ["${var.BASTION_APP}-bastion"]
  }
}

data "aws_acm_certificate" "app_cert" {
  domain = var.DOMAIN_NAME
  # statuses = ["ISSUED", "PENDING_VALIDATION"]
  statuses = ["ISSUED"]
}

# data "aws_ecs_cluster" "selected" {
#   cluster_name = var.ECS_CLUSTER_NAME
# }

data "aws_subnets" "public_subnets" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.selected.id]
  }

  tags = {
    Private     = "false"
    Environment = var.ENVIRONMENT
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

# resource "local_file" "dd_nginx_status_conf" {
#   filename = "${path.module}/nginx.d/conf.yaml"
#
#   content = templatefile("${path.module}/configs/datadog.optimux.yaml", {
#     nginx_status_url = "http://${var.APP_NAME}:81/nginx_status"
#     env              = var.ENVIRONMENT
#     app_name         = var.APP_NAME
#   })
# }

resource "aws_cloudwatch_log_group" "ecs_exec_logs" {
  name              = "/aws/ecs/${var.APP_NAME}/exec"
  retention_in_days = 7
}

resource "aws_ecs_cluster" "selected" {
  name = "${var.APP_NAME}-ec2-cluster"

  configuration {
    execute_command_configuration {
      logging = "OVERRIDE"

      log_configuration {
        cloud_watch_log_group_name = aws_cloudwatch_log_group.ecs_exec_logs.name
      }
    }
  }
}

// IAM Role for ec2 to register itsalf to ecs
resource "aws_iam_role" "ecsInstanceRole" {
  name               = "${var.APP_NAME}-ecs-instance-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json
}

resource "aws_iam_policy" "metrics_access_policy" {
  name = "${var.APP_NAME}-ecs-metric-access"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow",
        Action = [
          "s3:ListBucket"
        ],
        Resource = "arn:aws:s3:::adsensum-${var.ENVIRONMENT}"
      },
      {
        Effect = "Allow",
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject"
        ],
        Resource = "arn:aws:s3:::adsensum-${var.ENVIRONMENT}/*"
      },
      {
        Effect = "Allow",
        Action = [
          "ec2:DescribeInstances"
        ],
        Resource = "*"
      }
    ]
  })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_iam_role_policy_attachment" "attach_s3_access_for_metrics" {
  role       = aws_iam_role.ecsInstanceRole.name
  policy_arn = aws_iam_policy.metrics_access_policy.arn
}

resource "aws_iam_instance_profile" "ecsInstanceProfile" {
  # name = "${var.APP_NAME}-ecs-instance-profile"
  name = aws_iam_role.ecsInstanceRole.name
  role = aws_iam_role.ecsInstanceRole.name
}

resource "aws_iam_role_policy_attachment" "ecsAssumeRole" {
  for_each = toset([
    "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy", # this is not needed
    "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role",
  ])

  role       = aws_iam_role.ecsInstanceRole.name
  policy_arn = each.value
}

resource "aws_iam_policy" "ecsCloudWatchLogsPolicy" {
  name   = "${var.APP_NAME}-ecs-aws-logs-policy"
  policy = data.aws_iam_policy_document.ecsCloudWatchLogsPolicy.json
}

// used by ecs to run the deployment related tasks
resource "aws_iam_role" "ecsTaskExecutionRole" {
  name               = "${var.APP_NAME}-ecs-task-execution-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_agent.json
}

resource "aws_iam_role_policy_attachment" "ecsTaskLogsAttachment" {
  role       = aws_iam_role.ecsTaskExecutionRole.name
  policy_arn = aws_iam_policy.ecsCloudWatchLogsPolicy.arn
}

resource "aws_iam_role_policy_attachment" "ecsExecutionAssumeRole" {
  for_each = toset([
    "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy",
    "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role", # this is not needed
  ])

  role       = aws_iam_role.ecsTaskExecutionRole.name
  policy_arn = each.value
}

resource "aws_iam_role_policy" "ecsTaskExecutionPolicy" {
  name = "${var.APP_NAME}-ecs-task-execution-policy"
  role = aws_iam_role.ecsTaskExecutionRole.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = [
          data.aws_secretsmanager_secret.dockerhub.arn
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "${aws_cloudwatch_log_group.ecs_exec_logs.arn}:*"
      }
    ]
  })
}

resource "aws_security_group" "alb_sg" {
  name   = "${var.APP_NAME}-alb-sg"
  vpc_id = data.aws_vpc.selected.id

  ingress {
    protocol    = "tcp"
    from_port   = 443
    to_port     = 443
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    protocol    = "tcp"
    from_port   = 80
    to_port     = 80
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Terraform   = true,
    Environment = var.ENVIRONMENT,
    Name        = "${var.APP_NAME}-alb-sg"
  }

  lifecycle {
    create_before_destroy = true
  }
}

data "aws_security_group" "bastion_sg" {
  name = "${var.BASTION_APP}-bastion-sg"
}

resource "aws_security_group" "ecs_ec2_sg" {
  name   = "${var.APP_NAME}-ecs-ec2-sg"
  vpc_id = data.aws_vpc.selected.id

  ingress {
    protocol  = "tcp"
    from_port = 22
    to_port   = 65535
    security_groups = [
      aws_security_group.alb_sg.id,
      data.aws_security_group.bastion_sg.id
    ]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Terraform   = true,
    Environment = var.ENVIRONMENT,
    Name        = "${var.APP_NAME}-ecs-ec2-sg"
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_security_group" "efs_sg" {
  name        = "${var.APP_NAME}-efs-sg"
  description = "Allow NFS access from ECS instances"
  vpc_id      = data.aws_vpc.selected.id

  ingress {
    from_port       = 2049
    to_port         = 2049
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs_ec2_sg.id]
    description     = "Allow NFS from ECS instances"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# resource "aws_security_group_rule" "efs_ingress_from_ecs" {
#   type                     = "ingress"
#   from_port                = 2049
#   to_port                  = 2049
#   protocol                 = "tcp"
#   security_group_id        = aws_security_group.ecs_ec2_sg.id
#   source_security_group_id = aws_security_group.ecs_ec2_sg.id
#   description              = "Allow NFS from ECS EC2 instances"
# }

locals {
  dd_probes_remote_url = "s3://adsensum-${var.ENVIRONMENT}/${var.DD_SERVICE}/${var.ENVIRONMENT}/datadog/conf.d/"

  ecs_sh_content = templatefile("${path.module}/ecs.sh", {
    ECS_CLUSTER_NAME      = aws_ecs_cluster.selected.name
    APP_NAME              = var.APP_NAME,
    DD_APP_NAME           = var.DD_APP_NAME,
    REMOTE_PROBES_CFG_DIR = local.dd_probes_remote_url,
    ECS_DISCOVERY_TAG     = var.ECS_DISCOVERY_TAG,
  })
}

resource "aws_key_pair" "media-key-pair" {
  key_name   = "${var.APP_NAME}-key-pair"
  public_key = tls_private_key.rsa.public_key_openssh
}

resource "tls_private_key" "rsa" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "local_file" "tf-key" {
  content  = tls_private_key.rsa.private_key_pem
  filename = "${var.APP_NAME}-key-pair.pem"
}

# resource "aws_cloudwatch_log_group" "ecs" {
#   name              = "/ecs/${var.APP_NAME}-service/"
#   retention_in_days = 7
# }

## Connecting EFS store

data "aws_efs_file_system" "app_logs" {
  creation_token = "${var.APP_NAME}-app-logs"
}

data "aws_efs_file_system" "image_cache" {
  creation_token = "${var.APP_NAME}-image-cache"
}

resource "aws_efs_mount_target" "app_logs" {
  for_each = toset(data.aws_subnets.private_subnets.ids)

  file_system_id  = data.aws_efs_file_system.app_logs.id
  subnet_id       = each.value
  security_groups = [aws_security_group.efs_sg.id]
}

resource "aws_efs_mount_target" "image_cache" {
  for_each = toset(data.aws_subnets.private_subnets.ids)

  file_system_id  = data.aws_efs_file_system.image_cache.id
  subnet_id       = each.value
  security_groups = [aws_security_group.efs_sg.id]
}

resource "aws_launch_template" "app_server_launch_configuration" {
  name_prefix            = var.APP_NAME
  image_id               = var.AMI_IMAGE_ID
  instance_type          = var.INSTANCE_TYPE
  key_name               = "${var.APP_NAME}-key-pair"
  vpc_security_group_ids = [aws_security_group.ecs_ec2_sg.id]

  iam_instance_profile {
    arn = aws_iam_instance_profile.ecsInstanceProfile.arn
  }

  monitoring {
    enabled = true
  }

  tag_specifications {
    resource_type = "instance"

    tags = {
      Terraform   = true,
      Name        = var.SERVICE_NAME,
      Type        = "server"
      Environment = var.ENVIRONMENT
      Version     = "${var.COMMIT_VERSION}"
    }
  }

  user_data = base64encode(local.ecs_sh_content)

  # cpu_options {
  #   core_count       = 2
  #   threads_per_core = 10
  # }

  # block_device_mappings {
  #   device_name = "/dev/xvdf"
  #
  #   ebs {
  #     volume_size           = 10
  #     volume_type           = "gp3"
  #     delete_on_termination = false
  #   }
  # }
  #
  # block_device_mappings {
  #   device_name = "/dev/xvdg"
  #
  #   ebs {
  #     volume_size           = 10
  #     volume_type           = "gp3"
  #     delete_on_termination = false
  #   }
  # }
}

resource "aws_ecs_task_definition" "server_task_definition" {
  family                   = var.APP_NAME
  task_role_arn            = aws_iam_role.ecsTaskExecutionRole.arn
  execution_role_arn       = aws_iam_role.ecsTaskExecutionRole.arn
  requires_compatibilities = ["EC2"]
  network_mode             = "bridge"

  # Enable ECS Exec logging
  runtime_platform {
    operating_system_family = "LINUX"
  }

  # volume {
  #   name      = "app_logs"
  #   host_path = "/mnt/log"
  # }
  #
  # volume {
  #   name      = "image_cache"
  #   host_path = "/mnt/cache"
  # }

  volume {
    name      = "proc"
    host_path = "/proc"
  }

  volume {
    name      = "sys"
    host_path = "/sys"
  }

  volume {
    name      = "docker_sock"
    host_path = "/var/run/docker.sock"
  }

  volume {
    name      = "dd_conf"
    host_path = "/tmp/datadog-agent/conf.d/"
  }

  volume {
    name = "app_logs"

    efs_volume_configuration {
      file_system_id     = data.aws_efs_file_system.app_logs.id
      root_directory     = "/"
      transit_encryption = "ENABLED"
    }
  }

  volume {
    name = "image_cache"

    efs_volume_configuration {
      file_system_id     = data.aws_efs_file_system.image_cache.id
      root_directory     = "/"
      transit_encryption = "ENABLED"
    }
  }

  volume {
    name = "edge_cache_volume"

    docker_volume_configuration {
      scope  = "task"
      driver = "local"
      driver_opts = {
        "type"   = "tmpfs"
        "device" = "tmpfs"
        "o"      = "size=1024m,uid=1000,gid=1000"
      }
    }
  }

  container_definitions = templatefile("${path.module}/ecs-task-definition.json.tmpl", {
    IMAGE : var.DOCKER_IMAGE,
    APP_NAME : var.APP_NAME,
    APP_VERSION : var.COMMIT_VERSION,
    APP_PORT : var.APP_PORT,
    AWS_REGION : var.AWS_REGION,
    S3_BASE_URL : var.S3_BASE_URL,
    DOCKER_CREDS : data.aws_secretsmanager_secret.dockerhub.arn,
    CORS_ORIGINS : var.CORS_ORIGINS,
    QSIZE : var.QSIZE,
    CLEANER_SIDECAR_IMAGE : var.CLEANER_SIDECAR_IMAGE,
    ENVIRONMENT : var.APP_ENVIRONMENT,
    DOMAIN : var.FULL_DOMAIN_NAME,
    PG_URL : var.PG_URL,
    DD_API_KEY : var.DD_API_KEY,
    DATADOG_SITE : var.DATADOG_SITE,
    DD_SERVICE : var.DD_SERVICE,
    STATSD_ADDR : var.STATSD_ADDR,
    REMOTE_PROBES_CFG_DIR : local.dd_probes_remote_url,
    FFMPEG_HWACCEL : var.FFMPEG_HWACCEL,
    FFMPEG_THREADS : var.FFMPEG_THREADS
  })
}

resource "aws_ecs_service" "app_ecs_service" {
  name            = var.APP_NAME
  cluster         = aws_ecs_cluster.selected.id
  task_definition = aws_ecs_task_definition.server_task_definition.arn

  launch_type            = "EC2"
  desired_count          = var.ECS_SERVICE_DESIRED_COUNT
  enable_execute_command = true # Enable ECS Exec for debugging

  # ordered_placement_strategy {
  #   type  = "binpack"
  #   field = "memory"
  # }

  ordered_placement_strategy {
    type  = "binpack"
    field = "cpu"
  }

  force_new_deployment = true
  triggers = {
    redeployment = plantimestamp()
  }


  load_balancer {
    target_group_arn = aws_lb_target_group.media_lb_tg.arn
    container_name   = var.APP_NAME
    container_port   = 80
  }

  depends_on = [aws_autoscaling_group.app_server_ecs_asg]

  wait_for_steady_state = true
  
  lifecycle {
    ignore_changes = [desired_count]
  }
}

# ECS Service Auto Scaling Target
resource "aws_appautoscaling_target" "ecs_service_target" {
  max_capacity       = var.ECS_SERVICE_MAX_CAPACITY
  min_capacity       = var.ECS_SERVICE_MIN_CAPACITY
  resource_id        = "service/${aws_ecs_cluster.selected.name}/${aws_ecs_service.app_ecs_service.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

# Auto Scaling Policy - CPU Based
resource "aws_appautoscaling_policy" "ecs_service_cpu_policy" {
  name               = "${var.APP_NAME}-cpu-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs_service_target.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs_service_target.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs_service_target.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
    target_value       = 70.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

# Auto Scaling Policy - Memory Based
resource "aws_appautoscaling_policy" "ecs_service_memory_policy" {
  name               = "${var.APP_NAME}-memory-autoscaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs_service_target.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs_service_target.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs_service_target.service_namespace

  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageMemoryUtilization"
    }
    target_value       = 80.0
    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}

resource "aws_autoscaling_group" "app_server_ecs_asg" {
  name = "${var.APP_NAME}_asg"

  vpc_zone_identifier = data.aws_subnets.private_subnets.ids
  target_group_arns   = [aws_lb_target_group.media_lb_tg.arn]
  force_delete        = true

  launch_template {
    id = aws_launch_template.app_server_launch_configuration.id
    # version = aws_launch_template.app_server_launch_configuration.latest_version
    version = "$Latest"
  }

  min_size         = 1
  max_size         = 2 # changed: 5
  desired_capacity = 1

  health_check_type         = "EC2"
  health_check_grace_period = 300

  lifecycle {
    create_before_destroy = true
  }

  instance_refresh {
    strategy = "Rolling"

    preferences {
      min_healthy_percentage = 50
      max_healthy_percentage = 100 # changed: 150
      instance_warmup        = 50
      # skip_matching          = true
    }

    triggers = ["tag", "desired_capacity"]
  }

  tag {
    key                 = "Name"
    value               = var.APP_NAME
    propagate_at_launch = true
  }

  tag {
    key                 = "AmazonECSManaged"
    value               = true
    propagate_at_launch = true
  }

  tag {
    key                 = "ForceRedeploy"
    value               = timestamp()
    propagate_at_launch = true
  }

  tag {
    key                 = "Terraform"
    value               = true
    propagate_at_launch = true
  }


  tag {
    key                 = "Environment"
    value               = var.ENVIRONMENT
    propagate_at_launch = true
  }
}


resource "aws_autoscaling_policy" "ecs_cpu_scale_up" {
  name                   = "${var.APP_NAME}-scale-up"
  scaling_adjustment     = 1
  adjustment_type        = "ChangeInCapacity"
  cooldown               = 60
  autoscaling_group_name = aws_autoscaling_group.app_server_ecs_asg.name
}

resource "aws_lb" "media_lb" {
  count = var.ENVIRONMENT == "production" ? 0 : 1

  name               = "${var.APP_NAME}-lb"
  internal           = false
  load_balancer_type = "application"

  preserve_host_header = true

  security_groups = [aws_security_group.alb_sg.id]
  subnets         = data.aws_subnets.public_subnets.ids

  enable_deletion_protection = false
  enable_http2               = true

  tags = {
    Environment = var.ENVIRONMENT
    Name        = "${var.APP_NAME}-${var.LB_SUFFIX}"
    Terraform   = true
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_lb" "media_lb2" {
  count = var.ENVIRONMENT == "production" ? 1 : 0

  name               = "${var.APP_NAME}-lb2"
  internal           = false
  load_balancer_type = "application"

  preserve_host_header = true

  security_groups = [aws_security_group.alb_sg.id]
  subnets         = data.aws_subnets.public_subnets.ids

  enable_deletion_protection = false
  enable_http2               = true

  tags = {
    Environment = var.ENVIRONMENT
    Name        = "${var.APP_NAME}-lb-2"
    Terraform   = true
  }

  lifecycle {
    create_before_destroy = true
  }
}

locals {
  selected_media_lb = var.ENVIRONMENT == "production" ? aws_lb.media_lb2[0] : aws_lb.media_lb[0]
}

resource "aws_lb_target_group" "media_lb_tg" {
  # name = "${var.APP_NAME}-lb-tg-prod"
  name_prefix = "media-"
  port        = 80
  protocol    = "HTTP"

  vpc_id = data.aws_vpc.selected.id

  health_check {
    healthy_threshold   = 2 # changed: 3
    unhealthy_threshold = 10
    timeout             = 10
    interval            = 30
    path                = "/optimux/ping"
    matcher             = "200-299"
    port                = var.APP_PORT
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Environment = var.ENVIRONMENT,
    Terraform   = true,
  }
}

# resource "aws_lb_target_group" "ssl_media_lb_tg" {
#   name             = "${var.APP_NAME}-ssl-lb-tg"
#   port             = 443
#   protocol         = "HTTPS"
#   protocol_version = "HTTP2"
#
#   vpc_id = data.aws_vpc.selected.id
#
#   health_check {
#     healthy_threshold   = 3
#     unhealthy_threshold = 10
#     timeout             = 10
#     interval            = 30
#     path                = "/${var.SERVICE_NAME}/ping"
#     port                = 443
#     matcher             = "200-299"
#   }
# }

resource "aws_lb_listener" "media_ssl_lb_listener" {
  load_balancer_arn = local.selected_media_lb.arn
  port              = "443"
  protocol          = "HTTPS"
  certificate_arn   = data.aws_acm_certificate.app_cert.arn
  ssl_policy        = "ELBSecurityPolicy-2016-08"
  # alpn_policy       = "HTTP2Preferred"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.media_lb_tg.arn
  }
}

resource "aws_lb_listener" "ecs_alb_listener" {
  load_balancer_arn = local.selected_media_lb.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"

    redirect {
      protocol    = "HTTPS"
      port        = "443"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_listener_rule" "ssl_app_server_lb" {
  listener_arn = aws_lb_listener.ecs_alb_listener.arn
  priority     = 100

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.media_lb_tg.arn
  }

  condition {
    path_pattern {
      values = ["/${var.SERVICE_NAME}/*"]
    }
  }
}
