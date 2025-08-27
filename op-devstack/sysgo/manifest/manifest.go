package manifest

// This file is copied from netchef/pkg/manifest/manifest.go

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Manifest represents the top-level configuration for a network deployment
type Manifest struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	L1       L1Config `yaml:"l1"`
	L2       L2Config `yaml:"l2"`
	Features []string `yaml:"features"`
}

// L1Config represents the L1 chain configuration
type L1Config struct {
	Name    string `yaml:"name"`
	ChainID uint64 `yaml:"chain_id"`
}

// L2Config represents the L2 chain configuration
type L2Config struct {
	Deployment L2Deployment                `yaml:"deployment"`
	Components map[string]ComponentVersion `yaml:"components"`
	Chains     []Chain                     `yaml:"chains"`
}

// L2Deployment contains deployment-specific configuration
type L2Deployment struct {
	OpDeployer  DeployerConfig `yaml:"op-deployer"`
	L1Contracts Contracts      `yaml:"l1-contracts"`
	L2Contracts Contracts      `yaml:"l2-contracts"`
	Overrides   Overrides      `yaml:"overrides"`
}

// DeployerConfig contains deployer version information
type DeployerConfig struct {
	Version string `yaml:"version"`
}

// Contracts represents contract artifact locations
type Contracts struct {
	Locator string `yaml:"locator"`
}

// Overrides contains timing configuration overrides
type Overrides struct {
	SecondsPerSlot     int `yaml:"seconds_per_slot"`
	FjordTimeOffset    int `yaml:"fjord_time_offset"`
	GraniteTimeOffset  int `yaml:"granite_time_offset"`
	HoloceneTimeOffset int `yaml:"holocene_time_offset"`
}

// ComponentVersion represents a versioned component
type ComponentVersion struct {
	Repository string `yaml:"repository,omitempty"`
	Version    string `yaml:"version"`
}

// Chain represents a chain configuration
type Chain struct {
	Name     string   `yaml:"name"`
	Id       string   `yaml:"chain_id"`
	Features []string `yaml:"features"`
}

// NewManifestFromPath reads and parses a manifest file from the given path
func NewManifestFromPath(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

func (m *Manifest) HasFeature(feature string) bool {
	for _, f := range m.Features {
		if f == feature {
			return true
		}
	}
	return false
}

func (c *Chain) HasFeature(feature string) bool {
	for _, f := range c.Features {
		if f == feature {
			return true
		}
	}
	return false
}

func (m *Manifest) HasChainFeature(chain string, feature string) bool {
	for _, c := range m.L2.Chains {
		if c.Name == chain {
			return c.HasFeature(feature)
		}
	}
	return false
}
