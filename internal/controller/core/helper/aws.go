package helper

import (
	"context"
	"strings"

	// "go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// zapCtrl "sigs.k8s.io/controller-runtime/pkg/log/zap"
	// "github.com/aws/aws-sdk-go-v2/aws"
	// ec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	// ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	// pricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	// pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	// corev1alpha1 "github.com/skycluster-project/skycluster-operator/api/core/v1alpha1"
)

// AWSKeysFromSecret retrieves AWS credentials from a Kubernetes Secret
func FetchAWSKeysFromSecret(ctx context.Context, c client.Client) (string, string, error) {
	namespace := "skycluster"
	// labelSelector := "skycluster.io/managed-by=skycluster, skycluster.io/provider-platform=aws"

	var secretList corev1.SecretList
	labelSelector := labels.SelectorFromSet(map[string]string{
		"skycluster.io/managed-by":        "skycluster",
		"skycluster.io/provider-platform": "aws",
		"skycluster.io/secret-role":       "credentials",
	})

	if err := c.List(ctx, &secretList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labelSelector},
	); err != nil {
		return "", "", err
	}

	if len(secretList.Items) == 0 {
		return "", "", nil // No secrets found
	}
	secret := secretList.Items[0] // Assuming the first secret is the one we want
	if secret.Data == nil {
		return "", "", nil // No data in the secret
	}
	if _, ok := secret.Data["aws_access_key_id"]; !ok {
		return "", "", nil // No AWS access key ID in the secret
	}
	if _, ok := secret.Data["aws_secret_access_key"]; !ok {
		return "", "", nil // No AWS secret access key in the secret
	}
	// Extract the AWS credentials from the secret
	if len(secret.Data["aws_access_key_id"]) == 0 || len(secret.Data["aws_secret_access_key"]) == 0 {
		return "", "", nil // Empty credentials
	}
	// Convert the byte slices to strings
	accessKeyID := strings.TrimSpace(string(secret.Data["aws_access_key_id"]))
	secretAccessKey := strings.TrimSpace(string(secret.Data["aws_secret_access_key"]))

	return accessKeyID, secretAccessKey, nil
}
