package sync_tester_ext_el

import (
	"testing"

	"github.com/ethereum-optimism/optimism/op-devstack/compat"
	"github.com/ethereum-optimism/optimism/op-devstack/presets"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

func TestMain(m *testing.M) {
	L2ELEndpoint := "https://sepolia.optimism.io/"
	L1CLBeaconEndpoint := "https://beacon-api-proxy-sepolia.primary.client.dev.oplabs.cloud"
	L1ELEndpoint := "https://proxyd-l1-sepolia.primary.client.dev.oplabs.cloud"
	L1ChainID := eth.ChainIDFromUInt64(11155111)

	presets.DoMain(m, presets.WithMinimalExternalELWithSuperchainRegistry(L1CLBeaconEndpoint, L1ELEndpoint, L2ELEndpoint, L1ChainID, "sepolia", eth.FCUState{
		Latest:    22285447,
		Safe:      22285447,
		Finalized: 22285447,
	}),
		presets.WithCompatibleTypes(compat.SysGo),
	)

}
