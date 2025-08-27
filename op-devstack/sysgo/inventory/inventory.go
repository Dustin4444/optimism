package inventory

// This file is directly copied from netchef/pkg/inventory/inventory.go

import (
	"fmt"
	"os"

	"github.com/ethereum-optimism/optimism/op-deployer/pkg/deployer/state"
	"gopkg.in/yaml.v3"
)

// Inventory represents the top-level inventory structure
type Inventory struct {
	Chains   []Chain   `yaml:"chains"`
	Services []Service `yaml:"services"`
}

// Chain represents a blockchain network configuration
type Chain struct {
	Nodes    []Node    `yaml:"nodes"`
	Services []Service `yaml:"services"`

	// hydrated fields
	Name   string             `yaml:"-"`
	Id     string             `yaml:"-"`
	State  *state.ChainState  `yaml:"-"`
	Intent *state.ChainIntent `yaml:"-"`
}

// Node represents a node in the network
type Node struct {
	Kind string       `yaml:"kind"`
	Name string       `yaml:"name"`
	Spec NodeSpec     `yaml:"spec"`
	Deps Dependencies `yaml:"deps"`

	// hydrated fields
	Chain Chain `yaml:"-"`
}

// NodeSpec defines the specification for a node
type NodeSpec struct {
	Kind        string          `yaml:"kind"`
	EL          Service         `yaml:"el"`
	CL          Service         `yaml:"cl"`
	Flashblocks FlashblocksSpec `yaml:"flashblocks,omitempty"`
}

// FlashblocksSpec holds the config for flashblocks
type FlashblocksSpec struct {
	RollupBoost Service `yaml:"rollup-boost,omitempty"`
	Builder     Service `yaml:"builder"`
}

// Service represents a service in the network
type Service struct {
	Kind string       `yaml:"kind"`
	Name string       `yaml:"name"`
	Spec ServiceSpec  `yaml:"spec"`
	Deps Dependencies `yaml:"deps"`
}

// ServiceSpec defines the specification for a service
type ServiceSpec struct {
	Kind string           `yaml:"kind"`
	Spec ServiceLayerSpec `yaml:"spec"`
}

// ServiceLayerSpec contains the specific configuration for a service
type ServiceLayerSpec struct {
	Version string            `yaml:"version"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// Dependencies defines the dependencies for a node or service
type Dependencies struct {
	Nodes    []string `yaml:"nodes"`
	Services []string `yaml:"services"`
}

func NewInventoryFromPath(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read inventory file: %w", err)
	}

	var inventory Inventory
	if err := yaml.Unmarshal(data, &inventory); err != nil {
		return nil, fmt.Errorf("failed to parse inventory yaml: %w", err)
	}

	return &inventory, nil
}

func (i *Inventory) ResolveDeps(s Service) ([]Node, []Service) {
	allNodes := []Node{}
	allServices := []Service{}
	for _, chain := range i.Chains {
		nodes, services := chain.ResolveDeps(s)
		allNodes = append(allNodes, nodes...)
		allServices = append(allServices, services...)
	}
	for _, service := range i.Services {
		for _, serviceName := range s.Deps.Services {
			if service.Name == serviceName {
				allServices = append(allServices, service)
			}
		}
	}
	return allNodes, allServices
}

func (c *Chain) ResolveDeps(s Service) ([]Node, []Service) {
	nodes := []Node{}
	services := []Service{}
	for _, node := range c.Nodes {
		for _, nodeName := range s.Deps.Nodes {
			if node.Name == nodeName {
				nodes = append(nodes, node)
			}
		}
	}
	for _, service := range c.Services {
		for _, serviceName := range s.Deps.Services {
			if service.Name == serviceName {
				services = append(services, service)
			}
		}
	}
	return nodes, services
}

func (c *Chain) FindServiceByKind(kind string) *Service {
	for _, service := range c.Services {
		if service.Kind == kind {
			return &service
		}
	}
	return nil
}

func (d *Dependencies) Empty() bool {
	return len(d.Nodes) == 0 && len(d.Services) == 0
}
