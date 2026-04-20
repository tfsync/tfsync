package stateprovider

import (
	"context"
	"fmt"
)

// AzureRMBackend is a placeholder for the Azure Blob remote state backend.
// TODO: implement — required secret keys: resource_group_name, storage_account_name, container_name, key
type AzureRMBackend struct{}

func (AzureRMBackend) Type() string { return "azurerm" }

func (AzureRMBackend) ConfigureBackendFile(_ context.Context, _ map[string]string) (string, error) {
	return "", fmt.Errorf("azurerm state backend not yet implemented")
}
