output "app_logs_efs_id" {
  value = aws_efs_file_system.app_logs.id
}

output "image_cache_efs_id" {
  value = aws_efs_file_system.image_cache.id
}
