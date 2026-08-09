#!/bin/bash
# This is a wrapper around terraform to run commands with
# project specific config
#
# to use this, first call
# source .terrawrap.sh load-env <env-file>
# ./terrwrap.sh <terraform commands>
#

set -e

AWS_PROFILE="${AWS_PROFILE}"

_TF_ENVFILE="${_TF_ENVFILE:-.env}"

function set_envfile() {
  if [[ -z "$1" ]]; then
    echo "env file name not provided"
  fi

  echo "$1"
  export _TF_ENVFILE="$1"
}

function aws_account_id() {
  if [[ ! -z "${AWS_ACCOUNT_ID}" ]]; then
    echo "${AWS_ACCOUNT_ID} aws account id already set"
  else
    echo "getting aws account id"

    value=$(aws sts \
      get-caller-identity \
      --query "Account" \
      --output text \
      --profile="${AWS_PROFILE}"\
    )
        export AWS_ACCOUNT_ID="${value}"
  fi
}

function load_env() {
  file="${_TF_ENVFILE}"

  if [[ ! -f "$file" ]]; then
    echo "_TF_ENVFILE variable needs to be set"
    exit 1
  fi

  # echo "laoding from $file"

  declare -a env_vars

  while IFS= read -r line; do
    exported_var=$(echo "$line" | envsubst)
    # echo "Exported Variable: $exported_var"
    env_vars+=("$exported_var")
  done < <(grep -v '^#' "$file")

  export "${env_vars[@]}"
  
  # echo "profile $TF_VAR_AWS_PROFILE"
  # echo "app name $TF_VAR_APP_NAME" 
  # echo "env values exported"
}

function get_last_commit_id() {
  last_commit_id=$(git log -1 --pretty=format:"%h")
  if [[ -z "${last_commit_id}" ]]; then
    echo "not a git repository?"
    exit 1
  fi

  TF_VAR_COMMIT_VERSION="${last_commit_id}"
}

function apply_plan() {
  local apply_args=()

  # Find the -out=... argument to extract the plan file path
  for arg in "$@"; do
    if [[ "$arg" == -out=* ]]; then
      PLAN_FILE="${arg#-out=}"
    else
      apply_args+=("$arg")
    fi
  done

  if [[ -z "$PLAN_FILE" ]]; then
    echo "Missing -out=path.tfplan in arguments"
    exit 1
  fi

  echo "📦 Running terraform plan with ${PLAN_FILE}..."
  terraform plan "$@"

  echo "🚀 Applying saved plan: $PLAN_FILE"
  read -p "🚨 Do you want to apply this plan? (y/N): " confirm
  confirm=${confirm:-N}

  if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
    echo "❌ Apply cancelled."
    exit 0
  fi

  terraform apply "${apply_args[@]}" "$PLAN_FILE"
}

if [[ -z "${AWS_PROFILE}" ]]; then
  echo "aws_profile is missing. will not set to default"
  exit 1
fi

if [[ "$1" == "load-env" ]]; then
  echo "setting env file"
  set_envfile "$2"
  echo "ENVFILE set to ${_TF_ENVFILE}"
elif [[ "$1" == "get-env" ]]; then
  echo "ENVFILE set to ${_TF_ENVFILE}"
else
  aws_account_id
  load_env
  get_last_commit_id

  if [[ -z "$TF_VAR_AWS_ACCOUNT" || -z "$TF_VAR_APP_NAME" || -z "$TF_VAR_COMMIT_VERSION" ]]; then
      echo "env values not exported"
      echo "required"
      echo "TF_VAR_AWS_ACCOUNT (account_id)"
      echo "TF_VAR_APP_NAME"
      echo "TF_VAR_COMMIT_VERSION"
      exit 1
  fi

  echo "commit version $TF_VAR_COMMIT_VERSION"

  if [[ "$1" == "applyplan" ]]; then
    shift

    apply_plan "$@"
  else
    terraform "$@"
  fi
fi
