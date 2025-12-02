# Quick Start: EKS Pod Identity Setup

This is a condensed guide for setting up EKS Pod Identity for the CLS Backend. For detailed explanations, see `EKS_POD_IDENTITY_SETUP.md`.

## Prerequisites

- EKS cluster (Kubernetes 1.24+)
- AWS CLI v2.13.0+
- kubectl configured

## Quick Setup Commands

### 1. Set Variables

```bash
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export AWS_REGION="us-east-1"
export CLUSTER_NAME="cls-backend-cluster"
export NAMESPACE="cls-system"
export SERVICE_ACCOUNT="cls-backend-sa"
export ROLE_NAME="CLSBackendRole"
export SNS_TOPIC_ARN="arn:aws:sns:${AWS_REGION}:${AWS_ACCOUNT_ID}:cluster-events"
```

### 2. Create Trust Policy

```bash
cat > trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "pods.eks.amazonaws.com"
      },
      "Action": [
        "sts:AssumeRole",
        "sts:TagSession"
      ]
    }
  ]
}
EOF
```

### 3. Create IAM Role

```bash
aws iam create-role \
  --role-name ${ROLE_NAME} \
  --assume-role-policy-document file://trust-policy.json \
  --description "Role for CLS Backend pods"
```

### 4. Create SNS Policy

```bash
cat > sns-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sns:Publish",
        "sns:GetTopicAttributes"
      ],
      "Resource": "${SNS_TOPIC_ARN}"
    }
  ]
}
EOF

aws iam create-policy \
  --policy-name CLSBackendSNSAccess \
  --policy-document file://sns-policy.json
```

### 5. Attach Policy to Role

```bash
aws iam attach-role-policy \
  --role-name ${ROLE_NAME} \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/CLSBackendSNSAccess
```

### 6. Create Service Account

```bash
kubectl create namespace ${NAMESPACE} 2>/dev/null || true

kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SERVICE_ACCOUNT}
  namespace: ${NAMESPACE}
EOF
```

### 7. Create Pod Identity Association

```bash
aws eks create-pod-identity-association \
  --cluster-name ${CLUSTER_NAME} \
  --namespace ${NAMESPACE} \
  --service-account ${SERVICE_ACCOUNT} \
  --role-arn arn:aws:iam::${AWS_ACCOUNT_ID}:role/${ROLE_NAME}
```

### 8. Verify Setup

```bash
# List associations
aws eks list-pod-identity-associations \
  --cluster-name ${CLUSTER_NAME} \
  --namespace ${NAMESPACE}

# Verify role
aws iam get-role --role-name ${ROLE_NAME} \
  --query 'Role.AssumeRolePolicyDocument'
```

## Test Pod Identity

Deploy a test pod:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-identity
  namespace: ${NAMESPACE}
spec:
  serviceAccountName: ${SERVICE_ACCOUNT}
  containers:
  - name: aws-cli
    image: amazon/aws-cli:latest
    command: ["sleep", "3600"]
EOF
```

Verify credentials:

```bash
# Should show the IAM role ARN
kubectl exec -it test-pod-identity -n ${NAMESPACE} -- \
  aws sts get-caller-identity

# Should list SNS topics
kubectl exec -it test-pod-identity -n ${NAMESPACE} -- \
  aws sns list-topics
```

Clean up test pod:

```bash
kubectl delete pod test-pod-identity -n ${NAMESPACE}
```

## Deploy CLS Backend

Now deploy the CLS Backend using the deployment manifest:

```bash
kubectl apply -f deploy/kubernetes/aws-deployment.yaml
```

The deployment will automatically use Pod Identity for AWS authentication!

## Troubleshooting

**Pod can't assume role?**
```bash
# Check association exists
aws eks list-pod-identity-associations \
  --cluster-name ${CLUSTER_NAME} \
  --namespace ${NAMESPACE}

# Check pod logs
kubectl logs -n ${NAMESPACE} <pod-name>
```

**Permission denied?**
```bash
# Verify policy is attached
aws iam list-attached-role-policies --role-name ${ROLE_NAME}

# Check policy permissions
aws iam get-policy-version \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/CLSBackendSNSAccess \
  --version-id v1
```

## Clean Up

To remove everything:

```bash
# Delete pod identity association (get ID from list command)
aws eks delete-pod-identity-association \
  --cluster-name ${CLUSTER_NAME} \
  --association-id <association-id>

# Detach policy
aws iam detach-role-policy \
  --role-name ${ROLE_NAME} \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/CLSBackendSNSAccess

# Delete role
aws iam delete-role --role-name ${ROLE_NAME}

# Delete policy
aws iam delete-policy \
  --policy-arn arn:aws:iam::${AWS_ACCOUNT_ID}:policy/CLSBackendSNSAccess
```

## Key Differences from IRSA

| Aspect | Pod Identity | IRSA |
|--------|-------------|------|
| Service Account | No annotations needed | Requires `eks.amazonaws.com/role-arn` |
| Trust Policy | Simple (`pods.eks.amazonaws.com`) | Complex (OIDC conditions) |
| OIDC Provider | Not required | Required |
| Setup Steps | 7 commands | 10+ commands + OIDC setup |

## Next Steps

- Deploy CLS Backend: `kubectl apply -f deploy/kubernetes/aws-deployment.yaml`
- Check logs: `kubectl logs -n cls-system deployment/cls-backend`
- Test API: See `docs/AWS_DEPLOYMENT_GUIDE.md`

For detailed documentation, see:
- Full guide: `docs/EKS_POD_IDENTITY_SETUP.md`
- Deployment guide: `docs/AWS_DEPLOYMENT_GUIDE.md`
