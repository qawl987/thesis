/*
Copyright 2024 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// PorchClient provides methods to interact with the Nephio Porch API
// for managing KRM packages in a GitOps workflow.
type PorchClient struct {
	// Namespace where PackageRevisions are stored (typically "default").
	Namespace string

	// PublishedPackageID is the ID of the currently published srsran-gnb package.
	// Example: "regional.srsran-gnb.packagevariant-1"
	PublishedPackageID string

	// TempDir is the temporary directory for pulling/pushing packages.
	TempDir string
}

// SliceConfig represents the slicing configuration to be applied to SrsRANCellConfig.
type SliceConfig struct {
	SST               uint32
	SD                uint32
	MinPrbPolicyRatio uint32
	MaxPrbPolicyRatio uint32
	Priority          uint32
}

// toUint32 converts interface{} to uint32, handling both int and float64 from YAML parsing.
func toUint32(v interface{}) uint32 {
	switch val := v.(type) {
	case int:
		return uint32(val)
	case int64:
		return uint32(val)
	case float64:
		return uint32(val)
	case uint32:
		return val
	default:
		return 0
	}
}

// NewPorchClient creates a new Porch client with the given configuration.
func NewPorchClient(namespace, publishedPackageID string) *PorchClient {
	return &PorchClient{
		Namespace:          namespace,
		PublishedPackageID: publishedPackageID,
		TempDir:            "/tmp/porch-workdir",
	}
}

// UpdateRANSliceConfigs performs the complete Nephio Porch workflow to update
// the RAN slice configuration with ALL slices at once:
// 1. Copy the published package to create a new draft
// 2. Pull the draft to local filesystem
// 3. Mutate the srscellconfig.yaml with all slice configs
// 4. Push the changes back to the draft
// 5. Propose the package
// 6. Approve the package
func (p *PorchClient) UpdateRANSliceConfigs(ctx context.Context, configs []SliceConfig) error {
	if len(configs) == 0 {
		return fmt.Errorf("no slice configs provided")
	}

	// Generate a unique workspace name based on timestamp
	workspace := fmt.Sprintf("intent-%s", time.Now().Format("20060102-150405"))
	draftPkgID := fmt.Sprintf("regional.srsran-gnb.%s", workspace)
	localDir := filepath.Join(p.TempDir, workspace)

	// Ensure temp directory exists
	if err := os.MkdirAll(p.TempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Step 1: Copy the published package to create a new draft
	if err := p.copyPackage(ctx, workspace); err != nil {
		return fmt.Errorf("failed to copy package: %w", err)
	}

	// Step 2: Pull the draft to local filesystem
	if err := p.pullPackage(ctx, draftPkgID, localDir); err != nil {
		return fmt.Errorf("failed to pull package: %w", err)
	}

	// Step 3: Mutate the srscellconfig.yaml with ALL slices
	if err := p.mutateSliceConfigs(localDir, configs); err != nil {
		// Cleanup on failure
		os.RemoveAll(localDir)
		return fmt.Errorf("failed to mutate slice config: %w", err)
	}

	// Step 4: Push the changes back
	if err := p.pushPackage(ctx, draftPkgID, localDir); err != nil {
		os.RemoveAll(localDir)
		return fmt.Errorf("failed to push package: %w", err)
	}

	// Step 5: Propose the package
	if err := p.proposePackage(ctx, draftPkgID); err != nil {
		os.RemoveAll(localDir)
		return fmt.Errorf("failed to propose package: %w", err)
	}

	// Step 6: Approve the package
	if err := p.approvePackage(ctx, draftPkgID); err != nil {
		os.RemoveAll(localDir)
		return fmt.Errorf("failed to approve package: %w", err)
	}

	// Cleanup
	os.RemoveAll(localDir)

	fmt.Printf("[Porch] Successfully updated RAN with %d slice configs\n", len(configs))
	for _, cfg := range configs {
		fmt.Printf("  - SST=%d, SD=%d, maxPRB=%d, priority=%d\n",
			cfg.SST, cfg.SD, cfg.MaxPrbPolicyRatio, cfg.Priority)
	}

	return nil
}

// UpdateRANSliceConfig is kept for backward compatibility but calls UpdateRANSliceConfigs
func (p *PorchClient) UpdateRANSliceConfig(ctx context.Context, config SliceConfig) error {
	return p.UpdateRANSliceConfigs(ctx, []SliceConfig{config})
}

// copyPackage creates a new draft by copying the published package.
// Equivalent to: porchctl rpkg copy -n default <PUBLISHED_PACKAGE_ID> --workspace <WORKSPACE>
func (p *PorchClient) copyPackage(ctx context.Context, workspace string) error {
	cmd := exec.CommandContext(ctx, "porchctl", "rpkg", "copy",
		"-n", p.Namespace,
		p.PublishedPackageID,
		"--workspace", workspace)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("porchctl rpkg copy failed: %v, stderr: %s", err, stderr.String())
	}

	fmt.Printf("[Porch] Created draft package with workspace: %s\n", workspace)
	return nil
}

// pullPackage pulls the draft package to a local directory.
// Equivalent to: porchctl rpkg pull -n default <DRAFT_PACKAGE_ID> <LOCAL_DIR>
func (p *PorchClient) pullPackage(ctx context.Context, draftPkgID, localDir string) error {
	cmd := exec.CommandContext(ctx, "porchctl", "rpkg", "pull",
		"-n", p.Namespace,
		draftPkgID,
		localDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("porchctl rpkg pull failed: %v, stderr: %s", err, stderr.String())
	}

	fmt.Printf("[Porch] Pulled package to: %s\n", localDir)
	return nil
}

// mutateSliceConfigs modifies the srscellconfig.yaml with ALL slice configs at once.
// Uses map[string]interface{} to preserve all original YAML fields.
func (p *PorchClient) mutateSliceConfigs(localDir string, configs []SliceConfig) error {
	// Find the srscellconfig.yaml file
	cellConfigPath := filepath.Join(localDir, "srscellconfig.yaml")

	// Check if file exists
	data, err := os.ReadFile(cellConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read srscellconfig.yaml: %w", err)
	}

	// Parse the YAML into a generic map to preserve all fields
	var cellConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &cellConfig); err != nil {
		return fmt.Errorf("failed to parse srscellconfig.yaml: %w", err)
	}

	// Get or create spec section
	spec, ok := cellConfig["spec"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("srscellconfig.yaml missing or invalid 'spec' section")
	}

	// Build the complete slicing array from all configs
	slicing := make([]interface{}, 0, len(configs))
	for _, config := range configs {
		newSlice := map[string]interface{}{
			"sst": config.SST,
			"sd":  config.SD,
			"schedCfg": map[string]interface{}{
				"minPrbPolicyRatio": config.MinPrbPolicyRatio,
				"maxPrbPolicyRatio": config.MaxPrbPolicyRatio,
				"priority":          config.Priority,
			},
		}
		slicing = append(slicing, newSlice)
	}

	// Replace slicing with new complete array
	spec["slicing"] = slicing
	cellConfig["spec"] = spec

	// Marshal back to YAML
	newData, err := yaml.Marshal(&cellConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal srscellconfig.yaml: %w", err)
	}

	// Write back to file
	if err := os.WriteFile(cellConfigPath, newData, 0644); err != nil {
		return fmt.Errorf("failed to write srscellconfig.yaml: %w", err)
	}

	fmt.Printf("[Porch] Mutated srscellconfig.yaml with %d slice configs\n", len(configs))
	return nil
}

// pushPackage pushes the local changes back to the draft package.
// Equivalent to: porchctl rpkg push -n default <DRAFT_PACKAGE_ID> <LOCAL_DIR>
func (p *PorchClient) pushPackage(ctx context.Context, draftPkgID, localDir string) error {
	cmd := exec.CommandContext(ctx, "porchctl", "rpkg", "push",
		"-n", p.Namespace,
		draftPkgID,
		localDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("porchctl rpkg push failed: %v, stderr: %s", err, stderr.String())
	}

	fmt.Printf("[Porch] Pushed changes to draft package: %s\n", draftPkgID)
	return nil
}

// proposePackage proposes the draft package for approval.
// Equivalent to: porchctl rpkg propose -n default <DRAFT_PACKAGE_ID>
func (p *PorchClient) proposePackage(ctx context.Context, draftPkgID string) error {
	cmd := exec.CommandContext(ctx, "porchctl", "rpkg", "propose",
		"-n", p.Namespace,
		draftPkgID)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("porchctl rpkg propose failed: %v, stderr: %s", err, stderr.String())
	}

	fmt.Printf("[Porch] Proposed package: %s\n", draftPkgID)
	return nil
}

// approvePackage approves the proposed package.
// Equivalent to: porchctl rpkg approve -n default <DRAFT_PACKAGE_ID>
func (p *PorchClient) approvePackage(ctx context.Context, draftPkgID string) error {
	cmd := exec.CommandContext(ctx, "porchctl", "rpkg", "approve",
		"-n", p.Namespace,
		draftPkgID)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("porchctl rpkg approve failed: %v, stderr: %s", err, stderr.String())
	}

	fmt.Printf("[Porch] Approved package: %s\n", draftPkgID)
	return nil
}

// GetPublishedPackageID discovers the currently published srsran-gnb package.
func (p *PorchClient) GetPublishedPackageID(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "packagerevisions",
		"-n", p.Namespace,
		"-o", "custom-columns=NAME:.metadata.name,LIFECYCLE:.spec.lifecycle",
		"--no-headers")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to list packagerevisions: %v, stderr: %s", err, stderr.String())
	}

	// Parse output to find srsran-gnb Published package
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			name := fields[0]
			lifecycle := fields[1]
			if strings.Contains(name, "srsran-gnb") && lifecycle == "Published" {
				return name, nil
			}
		}
	}

	return "", fmt.Errorf("no published srsran-gnb package found")
}

// PorchAPIClient is an alternative implementation using direct Kubernetes API
// instead of porchctl CLI. This is the recommended approach for production.
type PorchAPIClient struct {
	// This would use sigs.k8s.io/controller-runtime/pkg/client to interact with
	// PackageRevision and PackageRevisionResources CRDs directly.
	//
	// TODO: Implement using Porch Go SDK when available, or direct CR manipulation:
	// 1. Get PackageRevision CR for the published package
	// 2. Create a new PackageRevision CR with lifecycle=Draft (copy operation)
	// 3. Get/Update PackageRevisionResources to modify the package contents
	// 4. Update PackageRevision lifecycle to Proposed
	// 5. Update PackageRevision lifecycle to Published (approve operation)
}
