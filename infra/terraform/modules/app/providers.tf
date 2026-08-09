# terraform {
#   backend "s3" {
#     bucket         = "terraform-state-optimux"
#     region         = "us-east-1"
#     encrypt        = true
#     dynamodb_table = "terraform-locks"
#   }
# }
