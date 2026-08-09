variable "VPC_NAME" {
  type = string
}

variable "ENVIRONMENT" {
  type = string
}

variable "APP_NAME" {
  type = string
}

variable "AMI_IMAGE_ID" {
  type = string
}

data "aws_vpc" "selected" {
  tags = {
    Name = var.VPC_NAME
  }
}

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

resource "aws_security_group" "bastion_sg" {
  name        = "${var.APP_NAME}-${var.ENVIRONMENT}-bastion-sg"
  description = "Security group for the Bastion host"
  vpc_id      = data.aws_vpc.selected.id

  # Inbound: allow SSH only from your home or office IP
  ingress {
    description = "SSH from my IP"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Outbound: allow all
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "${var.APP_NAME}-${var.ENVIRONMENT}-bastion-sg"
    Environment = var.ENVIRONMENT
    Terraform   = true
  }
}

resource "aws_key_pair" "bastion_key" {
  key_name   = "${var.APP_NAME}-${var.ENVIRONMENT}-bastion-key"
  public_key = tls_private_key.rsa.public_key_openssh
}

resource "tls_private_key" "rsa" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "local_file" "tf-key" {
  content  = tls_private_key.rsa.private_key_pem
  filename = "${var.APP_NAME}-${var.ENVIRONMENT}-key-pair.pem"
}

resource "aws_instance" "bastion_host" {
  ami                    = var.AMI_IMAGE_ID
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.public_subnets.ids[0]
  vpc_security_group_ids = [aws_security_group.bastion_sg.id]
  key_name               = aws_key_pair.bastion_key.key_name

  associate_public_ip_address = true

  tags = {
    Name        = "${var.APP_NAME}-${var.ENVIRONMENT}-bastion"
    Environment = var.ENVIRONMENT
    Terraform   = true
  }
}


output "bastion_public_ip" {
  description = "The public IP address of the bastion host"
  value       = "ssh -i ${var.APP_NAME}-${var.ENVIRONMENT}-key-pair.pem ec2-user@${aws_instance.bastion_host.public_ip}"
}
