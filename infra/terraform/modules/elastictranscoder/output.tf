# Output values
output "input_bucket_name" {
  description = "Name of the input S3 bucket"
  value       = var.create_src_bucket ? aws_s3_bucket.input_bucket[0].bucket : var.input_bucket_name
}

output "output_bucket_name" {
  description = "Name of the output S3 bucket"
  value       = var.create_dst_bucket ? aws_s3_bucket.output_bucket[0].bucket : var.output_bucket_name
}

