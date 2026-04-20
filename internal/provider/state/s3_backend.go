package stateprovider

import (
	"context"
	"encoding/json"
	"fmt"
)

// S3Backend generates a backend.tf.json for the Terraform S3 remote state backend.
// Also supports MinIO and other S3-compatible stores via the optional "endpoint" key.
//
// Required secret keys: bucket, key, region
// Optional secret keys: dynamodb_table, kms_key_id, endpoint, workspace_key_prefix
type S3Backend struct{}

func (S3Backend) Type() string { return "s3" }

func (S3Backend) ConfigureBackendFile(_ context.Context, secrets map[string]string) (string, error) {
	bucket := secrets["bucket"]
	key := secrets["key"]
	region := secrets["region"]

	if bucket == "" || key == "" || region == "" {
		return "", fmt.Errorf("s3 backend secret must contain keys: bucket, key, region")
	}

	cfg := map[string]interface{}{
		"bucket":  bucket,
		"key":     key,
		"region":  region,
		"encrypt": true,
	}
	if v := secrets["dynamodb_table"]; v != "" {
		cfg["dynamodb_table"] = v
	}
	if v := secrets["kms_key_id"]; v != "" {
		cfg["kms_key_id"] = v
	}
	if v := secrets["workspace_key_prefix"]; v != "" {
		cfg["workspace_key_prefix"] = v
	}
	if v := secrets["endpoint"]; v != "" {
		// S3-compatible stores (MinIO, Ceph, etc.) require path-style addressing.
		cfg["endpoint"] = v
		cfg["force_path_style"] = true
	}

	out := map[string]interface{}{
		"terraform": map[string]interface{}{
			"backend": map[string]interface{}{
				"s3": cfg,
			},
		},
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal s3 backend config: %w", err)
	}
	return string(b), nil
}
