package sysgo

import (
	"fmt"

	"github.com/ethereum-optimism/optimism/op-devstack/stack"
	"github.com/ethereum-optimism/optimism/op-devstack/sysgo/inventory"
	"github.com/ethereum-optimism/optimism/op-devstack/sysgo/manifest"
	"github.com/ethereum-optimism/optimism/op-service/eth"
)

func WithInventoryAndManifest(inv *inventory.Inventory, manif *manifest.Manifest, l1ELID stack.L1ELNodeID, l1CLID stack.L1CLNodeID) stack.Option[*Orchestrator] {
	if len(inv.Services) > 0 {
		panic("sysgo does not support any non-chain services like op-supervisor and op-acceptor")
	}

	if len(manif.Features) > 0 {
		panic("sysgo does not understand any manifest.Features right now")
	}

	l1ChainID := eth.ChainIDFromUInt64(manif.L1.ChainID)
	for i, l2 := range manif.L2.Chains {
		inv.Chains[i].Name = l2.Name
		inv.Chains[i].Id = l2.Id
		// TODO Respect state/intents?
	}

	// TODO use proper error handling instead of panics

	opts := stack.Combine[*Orchestrator]()

	// TODO right now we ignore the env vars in the service layer spec. We should respect them.

	for _, l2 := range inv.Chains {
		l2ChainID, err := eth.ChainIDFromString(l2.Id)
		if err != nil {
			panic(err)
		}
		opts.Add(WithDeployerOptions(WithPrefundedL2(l1ChainID, l2ChainID)))
		for _, node := range l2.Nodes {
			if node.Spec.Flashblocks.Builder.Kind != "" || node.Spec.Flashblocks.RollupBoost.Kind != "" {
				panic("sysgo cannot deploy flashblocks")
			}

			l2ELID := stack.NewL2ELNodeID(node.Name, l2ChainID)
			switch node.Spec.EL.Kind {
			case "op-geth":
				opts.Add(WithOpGeth(l2ELID))
			case "op-reth":
				opts.Add(WithOpReth(l2ELID))
			default:
				panic("unknown el kind")
			}
			// TODO node.Spec.EL.Spec.Kind is full/archive. We should respect this.

			clOpts := make([]L2CLOption, 0)
			if node.Spec.Kind == "sequencer" {
				clOpts = append(clOpts, L2CLSequencer())
			} // TODO there is also "rpc" and "snapsync".

			l2CLID := stack.NewL2CLNodeID(node.Name, l2ChainID)
			switch node.Spec.CL.Kind {
			case "op-node":
				opts.Add(WithOpNode(l2CLID, l1CLID, l1ELID, l2ELID, clOpts...))
			default:
				// netchef configs don't permit kona
				panic("unknown cl kind")
			}
		}

		for _, service := range l2.Services {
			switch service.Kind {
			case "batcher":
				nodeDeps, serviceDeps := l2.ResolveDeps(service)
				if len(serviceDeps) > 0 {
					panic("expected no service dependencies for batcher")
				} else if len(nodeDeps) > 1 {
					// TODO make this a warning not an error?
					panic("sysgo only supports one node dependency for the batcher right now")
				}
				nodeDep := nodeDeps[0]
				batcherID := stack.NewL2BatcherID(service.Name, l2ChainID)
				l2ELID := stack.NewL2ELNodeID(nodeDep.Name, l2ChainID)
				l2CLID := stack.NewL2CLNodeID(nodeDep.Name, l2ChainID)
				opts.Add(WithBatcher(batcherID, l1ELID, l2CLID, l2ELID))
			case "proposer":
				nodeDeps, serviceDeps := l2.ResolveDeps(service)
				if len(serviceDeps) > 0 {
					panic("expected no service dependencies for batcher")
				} else if len(nodeDeps) != 1 {
					panic("proposer must have exactly one node dependency")
				}
				nodeDep := nodeDeps[0]
				proposerID := stack.NewL2ProposerID(service.Name, l2ChainID)
				l2CLID := stack.NewL2CLNodeID(nodeDep.Name, l2ChainID)
				opts.Add(WithProposer(proposerID, l1ELID, &l2CLID, nil))
			case "op-challenger":
				nodeDeps, serviceDeps := l2.ResolveDeps(service)
				if len(serviceDeps) > 0 {
					panic("expected no service dependencies for batcher")
				} else if len(nodeDeps) == 0 {
					panic("challenger must have at least one node dependency")
				}
				l2ELIDs := make([]stack.L2ELNodeID, 0, len(nodeDeps))
				l2CLIDs := make([]stack.L2CLNodeID, 0, len(nodeDeps))
				for _, node := range nodeDeps {
					l2ELIDs = append(l2ELIDs, stack.NewL2ELNodeID(node.Name, l2ChainID))
					l2CLIDs = append(l2CLIDs, stack.NewL2CLNodeID(node.Name, l2ChainID))
				}
				challengerID := stack.NewL2ChallengerID(service.Name, l2ChainID)
				opts.Add(WithL2Challenger(challengerID, l1ELID, l1CLID, nil, nil, &l2CLIDs[0], l2ELIDs))
			case "faucet":
				nodeDeps, serviceDeps := l2.ResolveDeps(service)
				if len(serviceDeps) > 0 {
					panic("expected no service dependencies for batcher")
				} else if len(nodeDeps) != 1 { // netchef only allows one faucet per chain
					panic("challenger must have exactly one node dependency")
				}
				l2ELID := stack.NewL2ELNodeID(nodeDeps[0].Name, l2ChainID)
				opts.Add(WithFaucets([]stack.L1ELNodeID{l1ELID}, []stack.L2ELNodeID{l2ELID}))
			default:
				panic(fmt.Errorf("service %s not supported by sysgo when deploying from inventory/manifest config files", service.Kind))
			}
		}
	}

	return opts
}
