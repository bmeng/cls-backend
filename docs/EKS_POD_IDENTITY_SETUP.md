# EKS Pod Identity Setup Guide

This guide explains how to set up EKS Pod Identity for the CLS Backend on AWS.

## What is EKS Pod Identity?

EKS Pod Identity is a simplified authentication mechanism that allows your Kubernetes pods to assume AWS IAM roles without requiring:
- OIDC provider configuration
- Service account annotations
- Complex IAM trust policies with OIDC conditions

It's the **recommended approach** for new EKS clusters (Kubernetes 1.24+).

## Pod Identity vs IRSA Comparison

| Feature | EKS Pod Identity | IRSA (IAM Roles for Service Accounts) |
|---------|------------------|---------------------------------------|
| **Setup Complexity** | Simple - 3 CLI commands | Complex - OIDC provider + annotations |
| **OIDC Provider** | Not required | Required |
| **Service Account Annotations** | Not required | Required (`eks.amazonaws.com/role-arn`) |
| **Trust Policy** | Simple (`pods.eks.amazonaws.com`) | Complex (OIDC conditions) |
| **Credential Rotation** | Automatic | Automatic |
| **Kubernetes Version** | 1.24+ | All versions |
| **AWS CLI Required** | Yes | eksctl handles it |

## Prerequisites

- EKS cluster running Kubernetes 1.24 or later
- AWS CLI version 2.13.0 or later
- `kubectl` configured to access your cluster
- Permissions to create IAM roles and policies

## Step-by-Step Setup

### 1. Enable Pod Identity on Your EKS Cluster

EKS Pod Identity should be enabled by default on new clusters. Verify:

```bash
aws eks describe-cluster \
  --name cls-backend-cluster \
  --query 'cluster.podIdentityAssociations'
```

If not enabled, Pod Identity is automatically activated when you create your first association.

### 2. Create IAM Trust Policy

Create a trust policy that allows the EKS Pod Identity service to assume the role:

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

**Key Point**: This is much simpler than IRSA trust policies that require OIDC provider URLs and conditions.

### 3. Create IAM Role

```bash
aws iam create-role \
  --role-name CLSBackendRole \
  --assume-role-policy-document file://trust-policy.json \
  --description "Role for CLS Backend pods to access AWS services"
```

### 4. Create IAM Policy for SNS Access

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
      "Resource": "arn:aws:sns:*:*:cluster-events"
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
  --role-name CLSBackendRole \
  --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/CLSBackendSNSAccess
```

Replace `<ACCOUNT_ID>` with your AWS account ID:
```bash
aws sts get-caller-identity --query Account --output text
```

### 6. Create Kubernetes Service Account

The service account doesn't need any special annotations:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cls-backend-sa
  namespace: cls-system
```

Apply it:
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cls-backend-sa
  namespace: cls-system
EOF
```

### 7. Create Pod Identity Association

This is the key step that links the service account to the IAM role:

```bash
aws eks create-pod-identity-association \
  --cluster-name cls-backend-cluster \
  --namespace cls-system \
  --service-account cls-backend-sa \
  --role-arn arn:aws:iam::<ACCOUNT_ID>:role/CLSBackendRole
```

Save the association ID from the output for later reference.

### 8. Verify Setup

```bash
# List all pod identity associations
aws eks list-pod-identity-associations \
  --cluster-name cls-backend-cluster

# Describe specific association
aws eks describe-pod-identity-association \
  --cluster-name cls-backend-cluster \
  --association-id <association-id>
```

## Testing Pod Identity

### 1. Deploy a Test Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-identity
  namespace: cls-system
spec:
  serviceAccountName: cls-backend-sa
  containers:
  - name: aws-cli
    image: amazon/aws-cli:latest
    command: ["sleep", "3600"]
```

Apply:
```bash
kubectl apply -f test-pod.yaml
```

### 2. Verify Pod Can Assume Role

```bash
# Check assumed identity
kubectl exec -it test-pod-identity -n cls-system -- \
  aws sts get-caller-identity

# Expected output shows the IAM role ARN
{
    "UserId": "AROAXXXXXXXXXXXXXXXXX:eks-cls-system-cls-backend-sa-xxxxx",
    "Account": "123456789012",
    "Arn": "arn:aws:sts::123456789012:assumed-role/CLSBackendRole/eks-cls-system-cls-backend-sa-xxxxx"
}
```

### 3. Test SNS Access

```bash
# List SNS topics
kubectl exec -it test-pod-identity -n cls-system -- \
  aws sns list-topics

# Publish test message (replace with your topic ARN)
kubectl exec -it test-pod-identity -n cls-system -- \
  aws sns publish \
  --topic-arn arn:aws:sns:us-east-1:123456789012:cluster-events \
  --message "Test message from pod identity"
```

## How It Works

1. **Pod Starts**: When a pod with the associated service account starts, EKS injects credentials into the pod
2. **Token Exchange**: The pod uses these credentials to request temporary AWS credentials from AWS STS
3. **Role Assumption**: STS returns temporary credentials for the IAM role
4. **AWS SDK**: The AWS SDK automatically uses these credentials for all API calls

The entire process is transparent to your application code!

## Environment Variables

The pod automatically receives these environment variables:

```bash
AWS_CONTAINER_CREDENTIALS_FULL_URI=<credential-endpoint>
AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE=/var/run/secrets/pods.eks.amazonaws.com/serviceaccount/eks-pod-identity-token
AWS_REGION=<cluster-region>
```

You don't need to configure these manually - they're injected automatically.

## Troubleshooting

### Issue: Pod can't assume role

**Check 1**: Verify pod identity association exists
```bash
aws eks list-pod-identity-associations \
  --cluster-name cls-backend-cluster \
  --namespace cls-system
```

**Check 2**: Verify IAM role trust policy
```bash
aws iam get-role --role-name CLSBackendRole \
  --query 'Role.AssumeRolePolicyDocument'
```

Expected trust policy principal: `pods.eks.amazonaws.com`

**Check 3**: Check pod logs
```bash
kubectl logs <pod-name> -n cls-system
```

Look for authentication errors.

### Issue: Permission denied errors

**Check**: Verify IAM policy is attached
```bash
aws iam list-attached-role-policies --role-name CLSBackendRole
```

**Check**: Verify policy has required permissions
```bash
aws iam get-policy-version \
  --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/CLSBackendSNSAccess \
  --version-id v1
```

### Issue: Wrong service account

**Check**: Pod is using the correct service account
```bash
kubectl get pod <pod-name> -n cls-system -o jsonpath='{.spec.serviceAccountName}'
```

## Updating Pod Identity

### Add New Permissions

To grant additional permissions, update the IAM policy:

```bash
# Get current policy version
POLICY_ARN="arn:aws:iam::<ACCOUNT_ID>:policy/CLSBackendSNSAccess"

# Create new policy version with additional permissions
aws iam create-policy-version \
  --policy-arn $POLICY_ARN \
  --policy-document file://updated-policy.json \
  --set-as-default
```

Pods will automatically pick up new permissions within minutes (no pod restart required).

### Delete Pod Identity Association

```bash
aws eks delete-pod-identity-association \
  --cluster-name cls-backend-cluster \
  --association-id <association-id>
```

## Best Practices

1. **One Role Per Application**: Create separate IAM roles for different applications
2. **Least Privilege**: Grant only required permissions
3. **Resource Restrictions**: Use resource ARNs in policies instead of wildcards
4. **Monitoring**: Enable CloudTrail to audit role usage
5. **Regular Reviews**: Periodically review and remove unused permissions

## Migrating from IRSA to Pod Identity

If you're currently using IRSA, migration is simple:

1. Create Pod Identity association (as shown above)
2. Remove service account annotation: `eks.amazonaws.com/role-arn`
3. Restart pods to pick up new credentials
4. Verify functionality
5. Delete OIDC provider (optional, if not used by other workloads)

## Additional Resources

- [AWS EKS Pod Identity Documentation](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html)
- [EKS Pod Identity vs IRSA Comparison](https://aws.amazon.com/blogs/containers/amazon-eks-pod-identity-a-new-way-for-applications-on-eks-to-obtain-iam-credentials/)
- [IAM Best Practices](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html)

## Summary

EKS Pod Identity provides a simpler, more streamlined way to grant AWS permissions to Kubernetes pods:

✅ **No OIDC provider required**
✅ **No service account annotations needed**
✅ **Simple trust policies**
✅ **Automatic credential rotation**
✅ **Works with existing IAM policies**
✅ **Better integration with EKS**

For new deployments, **EKS Pod Identity is the recommended approach**.
