#!/bin/bash

ROLE_ARN="arn:aws:iam::644293020023:user/deployment-manager"
ACTIONS=("ec2:RunInstances" "ec2:Describe*" "ec2:CreateSecurityGroup" "iam:PassRole")

for ACTION in "${ACTIONS[@]}"; do
    echo "Checking permission for action: $ACTION"
    aws iam simulate-principal-policy \
        --policy-source-arn "$ROLE_ARN" \
        --action-names "$ACTION" \
        --query "EvaluationResults[0].EvalDecision" \
        --output text
done
