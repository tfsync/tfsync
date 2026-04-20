// Package secretprovider contains SecretProvider implementations.
package secretprovider

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// K8sSecretProvider implements provider.SecretProvider using the
// controller-runtime cached client.
type K8sSecretProvider struct {
	Client client.Client
}

func (p *K8sSecretProvider) GetSecret(ctx context.Context, namespace, name string) (map[string]string, error) {
	var secret corev1.Secret
	if err := p.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	out := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		out[k] = string(v)
	}
	return out, nil
}
