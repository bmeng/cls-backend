# AWS Deployment Guide for CLS Backend

This guide provides comprehensive instructions for deploying the CLS Backend on AWS infrastructure.

## Architecture Overview

The CLS Backend has been updated to support both Google Cloud Platform (GCP) and Amazon Web Services (AWS). When deployed on AWS, it uses:

- **Amazon EKS** - Managed Kubernetes service for container orchestration
- **Amazon RDS PostgreSQL** - Managed PostgreSQL database
- **Amazon SNS + SQS** - Messaging service for event fan-out (replaces Google Cloud Pub/Sub)
- **EKS Pod Identity** - Simplified IAM authentication for pods (no OIDC provider needed)

### GCP vs AWS Architecture Comparison

| Component | GCP | AWS |
|-----------|-----|-----|
| **Kubernetes** | GKE | EKS |
| **Database** | Cloud SQL PostgreSQL | RDS PostgreSQL |
| **Messaging** | Cloud Pub/Sub | SNS + SQS |
| **Authentication** | Service Account Keys | EKS Pod Identity |
| **Container Registry** | GCR | ECR |
| **IAM Setup** | Service Account | IAM Role + Pod Identity Association |

## Prerequisites

### Required Tools
- AWS CLI v2 installed and configured
- kubectl installed
- eksctl installed (for EKS cluster management)
- docker or podman for building container images
- helm (optional, for Helm-based deployments)

### AWS Account Setup
- AWS account with appropriate permissions
- Access to create:
  - EKS clusters
  - RDS instances
  - SNS topics
  - SQS queues (created by controllers)
  - ECR repositories
  - IAM roles and policies

## AWS Infrastructure Setup

### 1. Create Amazon RDS PostgreSQL Instance

```bash
# Create RDS PostgreSQL instance
aws rds create-db-instance \
  --db-instance-identifier cls-backend-db \
  --db-instance-class db.t3.medium \
  --engine postgres \
  --engine-version 15.4 \
  --master-username clsadmin \
  --master-user-password <YOUR_STRONG_PASSWORD> \
  --allocated-storage 20 \
  --vpc-security-group-ids <YOUR_SECURITY_GROUP_ID> \
  --db-subnet-group-name <YOUR_DB_SUBNET_GROUP> \
  --backup-retention-period 7 \
  --preferred-backup-window "03:00-04:00" \
  --preferred-maintenance-window "mon:04:00-mon:05:00" \
  --storage-encrypted \
  --tags Key=Name,Value=cls-backend-db Key=Environment,Value=production
```

Get the database endpoint:
```bash
aws rds describe-db-instances \
  --db-instance-identifier cls-backend-db \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text
```

### 2. Create Amazon SNS Topic for Events

```bash
# Create SNS topic for cluster events
aws sns create-topic --name cluster-events

# Get the Topic ARN
aws sns list-topics --query 'Topics[?contains(TopicArn, `cluster-events`)].TopicArn' --output text
```

Save the Topic ARN for later use (e.g., `arn:aws:sns:us-east-1:123456789012:cluster-events`).

### 3. Create Amazon EKS Cluster

```bash
# Create EKS cluster using eksctl
eksctl create cluster \
  --name cls-backend-cluster \
  --region us-east-1 \
  --nodegroup-name cls-workers \
  --node-type t3.medium \
  --nodes 3 \
  --nodes-min 2 \
  --nodes-max 4 \
  --managed

# Verify cluster is running
kubectl get nodes
```

### 4. Create ECR Repository for Container Images

```bash
# Create ECR repository
aws ecr create-repository --repository-name cls-backend

# Get the repository URI
aws ecr describe-repositories \
  --repository-names cls-backend \
  --query 'repositories[0].repositoryUri' \
  --output text
```

### 5. Create IAM Role and Pod Identity for CLS Backend

```bash
# Create IAM trust policy for EKS Pod Identity
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

# Create IAM role with trust policy
aws iam create-role \
  --role-name CLSBackendRole \
  --assume-role-policy-document file://trust-policy.json \
  --description "Role for CLS Backend pods to access SNS"

# Create IAM policy for SNS access
cat > cls-backend-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sns:Publish",
        "sns:GetTopicAttributes"
      ],
      "Resource": "arn:aws:sns:us-east-1:123456789012:cluster-events"
    }
  ]
}
EOF

aws iam create-policy \
  --policy-name CLSBackendSNSAccess \
  --policy-document file://cls-backend-policy.json

# Attach policy to role
aws iam attach-role-policy \
  --role-name CLSBackendRole \
  --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/CLSBackendSNSAccess

# Create Pod Identity Association
aws eks create-pod-identity-association \
  --cluster-name cls-backend-cluster \
  --namespace cls-system \
  --service-account cls-backend-sa \
  --role-arn arn:aws:iam::<ACCOUNT_ID>:role/CLSBackendRole

# Verify the pod identity association
aws eks list-pod-identity-associations \
  --cluster-name cls-backend-cluster \
  --namespace cls-system
```

**Note**: EKS Pod Identity is simpler than IRSA as it doesn't require OIDC provider configuration.

**For detailed Pod Identity setup and troubleshooting**, see: `docs/EKS_POD_IDENTITY_SETUP.md`

## Building and Pushing Container Image

### 1. Build Container Image

```bash
cd cls-backend

# Build for linux/amd64 architecture (for EKS)
docker build --platform linux/amd64 -t cls-backend:latest .
```

### 2. Push to Amazon ECR

```bash
# Login to ECR
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com

# Tag image
docker tag cls-backend:latest \
  <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com/cls-backend:latest

# Push to ECR
docker push <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com/cls-backend:latest
```

## Kubernetes Deployment on EKS

### 1. Create Namespace

```bash
kubectl create namespace cls-system
```

### 2. Create Kubernetes Secrets

```bash
# Create database secret
kubectl create secret generic cls-backend-secrets \
  --from-literal=DATABASE_URL="postgres://clsadmin:<PASSWORD>@<RDS_ENDPOINT>:5432/cls?sslmode=require" \
  --from-literal=AWS_REGION="us-east-1" \
  --from-literal=AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN="arn:aws:sns:us-east-1:123456789012:cluster-events" \
  --namespace=cls-system
```

### 3. Create ConfigMap

```bash
kubectl create configmap cls-backend-config \
  --from-literal=MESSAGING_PROVIDER="aws" \
  --from-literal=AWS_USE_IAM_ROLE="true" \
  --from-literal=LOG_LEVEL="info" \
  --from-literal=ENVIRONMENT="production" \
  --namespace=cls-system
```

### 4. Deploy Application

Create `deployment-aws.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cls-backend
  namespace: cls-system
spec:
  replicas: 3
  selector:
    matchLabels:
      app: cls-backend
  template:
    metadata:
      labels:
        app: cls-backend
    spec:
      serviceAccountName: cls-backend-sa  # Uses IRSA for AWS credentials
      containers:
      - name: cls-backend
        image: <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com/cls-backend:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 8081
          name: metrics
        env:
        # Messaging provider configuration
        - name: MESSAGING_PROVIDER
          valueFrom:
            configMapKeyRef:
              name: cls-backend-config
              key: MESSAGING_PROVIDER
        - name: AWS_USE_IAM_ROLE
          valueFrom:
            configMapKeyRef:
              name: cls-backend-config
              key: AWS_USE_IAM_ROLE
        # AWS Configuration
        - name: AWS_REGION
          valueFrom:
            secretKeyRef:
              name: cls-backend-secrets
              key: AWS_REGION
        - name: AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN
          valueFrom:
            secretKeyRef:
              name: cls-backend-secrets
              key: AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN
        # Database Configuration
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: cls-backend-secrets
              key: DATABASE_URL
        # Application Configuration
        - name: LOG_LEVEL
          valueFrom:
            configMapKeyRef:
              name: cls-backend-config
              key: LOG_LEVEL
        - name: ENVIRONMENT
          valueFrom:
            configMapKeyRef:
              name: cls-backend-config
              key: ENVIRONMENT
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: cls-backend
  namespace: cls-system
spec:
  type: ClusterIP
  ports:
  - port: 80
    targetPort: 8080
    protocol: TCP
    name: http
  selector:
    app: cls-backend
---
apiVersion: v1
kind: Service
metadata:
  name: cls-backend-metrics
  namespace: cls-system
spec:
  type: ClusterIP
  ports:
  - port: 8081
    targetPort: 8081
    protocol: TCP
    name: metrics
  selector:
    app: cls-backend
```

Apply the deployment:
```bash
kubectl apply -f deployment-aws.yaml
```

### 5. Run Database Migrations

Create `migration-job.yaml`:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: cls-backend-migration
  namespace: cls-system
spec:
  template:
    metadata:
      labels:
        app: cls-backend-migration
    spec:
      restartPolicy: Never
      containers:
      - name: migration
        image: <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com/cls-backend:latest
        command: ["/bin/sh", "-c"]
        args:
          - |
            psql $DATABASE_URL -f /app/internal/database/migrations/001_complete_schema.sql
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: cls-backend-secrets
              key: DATABASE_URL
      serviceAccountName: cls-backend-sa
```

Apply the migration:
```bash
kubectl apply -f migration-job.yaml
kubectl logs -f job/cls-backend-migration -n cls-system
```

## Environment Variables Reference

### Required AWS Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `MESSAGING_PROVIDER` | Messaging provider type | `aws` |
| `AWS_REGION` | AWS region | `us-east-1` |
| `AWS_SNS_CLUSTER_EVENTS_TOPIC_ARN` | SNS Topic ARN for events | `arn:aws:sns:us-east-1:123456789012:cluster-events` |
| `AWS_USE_IAM_ROLE` | Use IAM role (IRSA) for authentication | `true` (recommended) |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://user:pass@rds-endpoint:5432/cls?sslmode=require` |

### Optional AWS Variables (if not using IAM role)

| Variable | Description |
|----------|-------------|
| `AWS_ACCESS_KEY_ID` | AWS access key |
| `AWS_SECRET_ACCESS_KEY` | AWS secret key |
| `AWS_SESSION_TOKEN` | AWS session token (for temporary credentials) |

## Verification

### 1. Check Deployment Status

```bash
kubectl get pods -n cls-system
kubectl get services -n cls-system
```

### 2. Check Logs

```bash
kubectl logs -f deployment/cls-backend -n cls-system
```

### 3. Test API Endpoints

```bash
# Port-forward for testing
kubectl port-forward service/cls-backend 8080:80 -n cls-system

# Test health endpoint
curl http://localhost:8080/health

# Test API info
curl http://localhost:8080/api/v1/info
```

### 4. Test Messaging (SNS)

Create a test cluster to verify SNS integration:
```bash
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Content-Type: application/json" \
  -H "X-User-Email: test@example.com" \
  -d '{
    "name": "test-cluster",
    "target_project_id": "test-project",
    "spec": {
      "version": "1.28",
      "region": "us-east-1"
    }
  }'
```

Check CloudWatch Logs for SNS publish confirmation.

## Monitoring and Observability

### CloudWatch Integration

The application logs will automatically appear in CloudWatch Logs when using AWS infrastructure.

### Prometheus Metrics

Metrics are exposed on port 8081:
```bash
kubectl port-forward service/cls-backend-metrics 8081:8081 -n cls-system
curl http://localhost:8081/metrics
```

## Troubleshooting

### Common Issues

1. **SNS Permission Errors**
   - Verify IAM policy has `sns:Publish` permission
   - Check Pod Identity Association exists: `aws eks list-pod-identity-associations --cluster-name <cluster> --namespace cls-system`
   - Verify IAM role trust policy allows `pods.eks.amazonaws.com` service
   - Check pod logs for authentication errors

2. **Database Connection Errors**
   - Verify RDS security group allows traffic from EKS
   - Check DATABASE_URL format includes `?sslmode=require`
   - Verify database credentials

3. **Image Pull Errors**
   - Ensure ECR repository policy allows EKS to pull images
   - Verify image exists in ECR

### Debug Commands

```bash
# Check pod events
kubectl describe pod <pod-name> -n cls-system

# Check service account
kubectl describe serviceaccount cls-backend-sa -n cls-system

# Check Pod Identity Association
aws eks describe-pod-identity-association \
  --cluster-name cls-backend-cluster \
  --association-id <association-id>

# List all pod identity associations
aws eks list-pod-identity-associations \
  --cluster-name cls-backend-cluster \
  --namespace cls-system

# Test SNS from pod
kubectl exec -it <pod-name> -n cls-system -- aws sns list-topics

# Check pod's assumed role
kubectl exec -it <pod-name> -n cls-system -- aws sts get-caller-identity
```

## Security Best Practices

1. **Use EKS Pod Identity** instead of access keys (no OIDC provider required)
2. **Enable RDS encryption** at rest
3. **Use SSL/TLS** for database connections (`sslmode=require`)
4. **Rotate credentials** regularly (Pod Identity handles token rotation automatically)
5. **Use AWS Secrets Manager** for sensitive data (optional enhancement)
6. **Enable VPC endpoints** for SNS to avoid internet traffic
7. **Use private subnets** for EKS worker nodes
8. **Apply least privilege** to IAM roles (only required SNS permissions)

## Cost Optimization

- Use **Auto Scaling** for EKS node groups
- Right-size RDS instance based on load
- Use **RDS Reserved Instances** for production
- Monitor SNS/SQS usage and costs
- Use **spot instances** for non-production workloads

## Next Steps

1. **Set up AWS Application Load Balancer** for external access
2. **Configure CloudWatch alarms** for monitoring
3. **Set up AWS Backup** for RDS
4. **Implement AWS WAF** for API protection
5. **Configure Route53** for DNS management
6. **Deploy controllers** with SQS subscriptions to cluster-events SNS topic

## Migration from GCP to AWS

If migrating from an existing GCP deployment:

1. **Export data** from GCP PostgreSQL
2. **Import data** to AWS RDS
3. **Update environment variables** to AWS configuration
4. **Deploy to EKS** following this guide
5. **Verify** all functionality works
6. **Update DNS** to point to new AWS endpoint
7. **Decommission** GCP resources after verification

## Support

For issues or questions:
- GitHub Issues: [cls-backend repository](https://github.com/apahim/cls-backend)
- Documentation: See `/docs` directory
