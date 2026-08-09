TF_STATE ?= tmp/optimux.tfstate
ENV ?= dev
VAR_FILE ?= dev.tfvars

plan:
	ENVIRONMENT=$(ENV) AWS_REGION=us-east-1 AWS_PROFILE=$(AWS_PROFILE) ./terrawrap.sh plan -var-file=$(VAR_FILE) -out=$(TF_STATE)

apply:
	ENVIRONMENT=$(ENV) AWS_REGION=us-east-1 AWS_PROFILE=$(AWS_PROFILE) ./terrawrap.sh apply -var-file=$(VAR_FILE) $(TF_STATE)
