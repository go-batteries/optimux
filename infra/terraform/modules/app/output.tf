data "aws_autoscaling_group" "asg" {
  name = aws_autoscaling_group.app_server_ecs_asg.name
}

data "aws_instances" "launched_instances" {
  depends_on = [aws_autoscaling_group.app_server_ecs_asg]

  filter {
    name   = "tag:Name"
    values = [var.APP_NAME]
  }
}

output "private_subnets" {
  value = data.aws_subnets.private_subnets.ids
}

output "public_subnets" {
  value = data.aws_subnets.public_subnets.ids
}

output "instance_ids" {
  value = data.aws_instances.launched_instances.ids
}

output "instance_ips" {
  value = data.aws_instances.launched_instances.private_ips
}

output "public_subnet_names" {
  value = data.aws_subnets.public_subnets.tags
}

output "aws_cluster_id" {
  value = aws_ecs_cluster.selected.id
}
