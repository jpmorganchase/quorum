package raft

import (
	"crypto/ecdsa"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type RaftService struct {
	blockchain     *core.BlockChain
	chainDb        ethdb.Database // Block chain database
	txMu           sync.Mutex
	txPool         *core.TxPool
	accountManager *accounts.Manager
	downloader     *downloader.Downloader

	raftProtocolManager *ProtocolManager
	startPeers          []*enode.Node

	// we need an event mux to instantiate the blockchain
	eventMux         *event.TypeMux
	minter           *minter
	nodeKey          *ecdsa.PrivateKey
	calcGasLimitFunc func(block *types.Block) uint64
}

// When we create a new Raft ProtocolManager this happens before we have access to the node id. The raftId is derived from the
// node id, so at the time of creation do not know the raft id yet.
func New(ctx *node.ServiceContext, chainConfig *params.ChainConfig, raftId uint64, raftjoinExisting bool, raftPort uint16, blockTime time.Duration, e *eth.Ethereum, startPeers []*enode.Node, datadir string, useDns bool) (*RaftService, error) {
	service := &RaftService{
		eventMux:         ctx.EventMux,
		chainDb:          e.ChainDb(),
		blockchain:       e.BlockChain(),
		txPool:           e.TxPool(),
		accountManager:   e.AccountManager(),
		downloader:       e.Downloader(),
		startPeers:       startPeers,
		nodeKey:          ctx.NodeKey(),
		calcGasLimitFunc: e.CalcGasLimit,
	}

	service.minter = newMinter(chainConfig, service, blockTime)

	var err error
	if service.raftProtocolManager, err = NewProtocolManager(raftId, raftPort, service.blockchain, service.eventMux, startPeers, raftjoinExisting, datadir, service.minter, service.downloader, useDns); err != nil {
		return nil, err
	}

	return service, nil
}

// Backend interface methods:

func (service *RaftService) AccountManager() *accounts.Manager { return service.accountManager }
func (service *RaftService) BlockChain() *core.BlockChain      { return service.blockchain }
func (service *RaftService) ChainDb() ethdb.Database           { return service.chainDb }
func (service *RaftService) DappDb() ethdb.Database            { return nil }
func (service *RaftService) EventMux() *event.TypeMux          { return service.eventMux }
func (service *RaftService) TxPool() *core.TxPool              { return service.txPool }

// node.Service interface methods:

func (service *RaftService) Protocols() []p2p.Protocol { return []p2p.Protocol{} }
func (service *RaftService) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: "raft",
			Version:   "1.0",
			Service:   NewPublicRaftAPI(service),
			Public:    true,
		},
	}
}

// Start implements node.Service, starting the background data propagation thread
// of the protocol.
func (service *RaftService) Start(p2pServer *p2p.Server) error {
	service.raftProtocolManager.Start(p2pServer)
	return nil
}

// Stop implements node.Service, stopping the background data propagation thread
// of the protocol.
func (service *RaftService) Stop() error {
	service.blockchain.Stop()
	service.raftProtocolManager.Stop()
	service.minter.stop()
	service.eventMux.Stop()

	service.chainDb.Close()

	log.Info("Raft stopped")
	return nil
}
