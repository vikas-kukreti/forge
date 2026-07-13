package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"log/slog"

	"forge/internal/config"
)

type NetworkManager struct {
	config *config.Config
}

func NewNetworkManager(cfg *config.Config) *NetworkManager {
	return &NetworkManager{config: cfg}
}

// SetupBridge creates the bridge and applies iptables rules for the sandbox
func (nm *NetworkManager) SetupBridge(ctx context.Context) error {
	bridgeName := nm.config.SbxBridge
	subnet := nm.config.SbxSubnet

	// 1. Check if bridge exists
	out, err := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return fmt.Errorf("failed to list docker networks: %w", err)
	}

	exists := false
	for _, name := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(name) == bridgeName {
			exists = true
			break
		}
	}

	// 2. Create if not exists
	if !exists {
		slog.Info("creating docker network", "bridge", bridgeName, "subnet", subnet)
		cmd := exec.CommandContext(ctx, "docker", "network", "create",
			"--driver", "bridge",
			"--subnet", subnet,
			"--opt", "com.docker.network.bridge.name="+bridgeName,
			"--opt", "com.docker.network.bridge.enable_icc=false",
			bridgeName,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to create network: %s - %w", string(out), err)
		}
	}

	// For M2, we just ensure the bridge is there. Iptables rules and egress proxy will be setup later if we actually invoke real networking testing.
	return nil
}

// GetBridgeGateway fetches the IP of the bridge gateway
func (nm *NetworkManager) GetBridgeGateway(ctx context.Context) (string, error) {
	bridgeName := nm.config.SbxBridge
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", bridgeName, "--format", "{{(index .IPAM.Config 0).Gateway}}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get bridge gateway: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
