package eth

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// Quorum: quorum_protocol enables the eth service to return two different protocols, one for the eth mainnet "eth" service,
//         and one for the quorum specific consensus algo, obtained from engine.consensus
//         2021 Jan in the future consensus (istanbul) may run from its own service and use a single subprotocol there,
//         instead of overloading the eth service.

var (
	// errEthPeerNil is returned when no eth peer is found to be associated with a p2p peer.
	errEthPeerNil           = errors.New("eth peer was nil")
	errEthPeerNotRegistered = errors.New("eth peer was not registered")
)

// quorum consensus Protocol variables are optionally set in addition to the "eth" protocol variables (eth/protocol.go).
var quorumConsensusProtocolName = ""

// ProtocolVersions are the supported versions of the quorum consensus protocol (first is primary), e.g. []uint{Istanbul64, Istanbul99, Istanbul100}.
var quorumConsensusProtocolVersions []uint

// protocol Length describe the number of messages support by the protocol/version map[uint]uint64{Istanbul64: 18, Istanbul99: 18, Istanbul100: 18}
var quorumConsensusProtocolLengths map[uint]uint64

// makeQuorumConsensusProtocol is similar to eth/handler.go -> makeProtocol. Called from eth/handler.go -> Protocols.
// returns the supported subprotocol to the p2p server.
// The Run method starts the protocol and is called by the p2p server. The quorum consensus subprotocol,
// leverages the peer created and managed by the "eth" subprotocol.
// The quorum consensus protocol requires that the "eth" protocol is running as well.
func (pm *ProtocolManager) makeQuorumConsensusProtocol(ProtoName string, version uint, length uint64) p2p.Protocol {

	return p2p.Protocol{
		Name:    ProtoName,
		Version: version,
		Length:  length,
		// no new peer created, uses the "eth" peer, so no peer management needed.
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			/*
			* 1. wait for the eth protocol to create and register an eth peer.
			* 2. get the associate eth peer that was registered by he "eth" protocol.
			* 2. add the rw protocol for the quorum subprotocol to the eth peer.
			* 3. start listening for incoming messages.
			* 4. the incoming message will be sent on the quorum specific subprotocol, e.g. "istanbul/100".
			* 5. send messages to the consensus engine handler.
			* 7. messages to other to other peers listening to the subprotocol can be sent using the
			*    (eth)peer.ConsensusSend() which will write to the protoRW.
			 */
			// wait for the "eth" protocol to create and register the peer (added to peerset)
			select {
			case <-p.EthPeerRegistered:
				// the ethpeer should be registered, try to retrieve it and start the consensus handler.
				p2pPeerId := fmt.Sprintf("%x", p.ID().Bytes()[:8])
				ethPeer := pm.peers.Peer(p2pPeerId)
				if ethPeer != nil {
					p.Log().Debug("consensus subprotocol retrieved eth peer from peerset", "ethPeer.id", ethPeer.id, "ProtoName", ProtoName)
					// add the rw protocol for the quorum subprotocol to the eth peer.
					ethPeer.addConsensusProtoRW(rw)
					return pm.handleConsensusLoop(p, rw)
				}
				p.Log().Error("consensus subprotocol retrieved nil eth peer from peerset", "ethPeer.id", ethPeer)
				return errEthPeerNil
			case <-p.EthPeerDisconnected:
				return errEthPeerNotRegistered
			}
		},
		NodeInfo: func() interface{} {
			return pm.NodeInfo()
		},
		PeerInfo: func(id enode.ID) interface{} {
			if p := pm.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
				return p.Info()
			}
			return nil
		},
	}
}

func (pm *ProtocolManager) handleConsensusLoop(p *p2p.Peer, protoRW p2p.MsgReadWriter) error {
	// Handle incoming messages until the connection is torn down
	for {
		if err := pm.handleConsensus(p, protoRW); err != nil {
			p.Log().Debug("Ethereum quorum message handling failed", "err", err)
			return err
		}
	}
}

// This is a no-op because the eth handleMsg main loop handle ibf message as well.
func (pm *ProtocolManager) handleConsensus(p *p2p.Peer, protoRW p2p.MsgReadWriter) error {
	// Read the next message from the remote peer (in protoRW), and ensure it's fully consumed
	msg, err := protoRW.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > protocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, protocolMaxMsgSize)
	}
	defer msg.Discard()

	// See if the consensus engine protocol can handle this message, e.g. istanbul will check for message is
	// istanbulMsg = 0x11, and NewBlockMsg = 0x07.
	handled, err := pm.handleConsensusMsg(p, msg)
	if handled {
		p.Log().Debug("consensus message was handled by consensus engine", "handled", handled,
			"quorumConsensusProtocolName", quorumConsensusProtocolName, "err", err)
		return err
	}

	return nil
}

func (pm *ProtocolManager) handleConsensusMsg(p *p2p.Peer, msg p2p.Msg) (bool, error) {
	if handler, ok := pm.engine.(consensus.Handler); ok {
		pubKey := p.Node().Pubkey()
		addr := crypto.PubkeyToAddress(*pubKey)
		handled, err := handler.HandleMsg(addr, msg)
		return handled, err
	}
	return false, nil
}

// makeLegacyProtocol is basically a copy of the eth makeProtocol, but for legacy subprotocols, e.g. "istanbul/99" "istabnul/64"
// If support legacy subprotocols is removed, remove this and associated code as well.
// If quorum is using a legacy protocol then the "eth" subprotocol should not be available.
func (pm *ProtocolManager) makeLegacyProtocol(protoName string, version uint, length uint64) p2p.Protocol {
	log.Debug("registering a legacy protocol ", "protoName", protoName)
	return p2p.Protocol{
		Name:    protoName,
		Version: version,
		Length:  length,
		Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
			peer := pm.newPeer(int(version), p, rw, pm.txpool.Get)
			peer.addConsensusProtoRW(rw)
			return pm.runPeer(peer, protoName)
		},
		NodeInfo: func() interface{} {
			return pm.NodeInfo()
		},
		PeerInfo: func(id enode.ID) interface{} {
			if p := pm.peers.Peer(fmt.Sprintf("%x", id[:8])); p != nil {
				return p.Info()
			}
			return nil
		},
	}
}

func (s *Ethereum) quorumConsensusProtocols() []p2p.Protocol {
	protos := make([]p2p.Protocol, len(quorumConsensusProtocolVersions))
	for i, vsn := range quorumConsensusProtocolVersions {
		// if we have a legacy protocol, e.g. istanbul/99, istanbul/64 then the protocol handler is will be the "eth"
		// protocol handler, and the subprotocol "eth" will not be used, but rather the legacy subprotocol will handle
		// both eth messages and consensus messages.
		if isLegacyProtocol(quorumConsensusProtocolName, vsn) {
			length, ok := quorumConsensusProtocolLengths[vsn]
			if !ok {
				panic("makeProtocol for unknown version")
			}
			lp := s.protocolManager.makeLegacyProtocol(quorumConsensusProtocolName, vsn, length)
			protos[i] = lp
		} else {
			length, ok := quorumConsensusProtocolLengths[vsn]
			if !ok {
				panic("makeQuorumConsensusProtocol for unknown version")
			}
			protos[i] = s.protocolManager.makeQuorumConsensusProtocol(quorumConsensusProtocolName, vsn, length)
		}
	}
	return protos
}

// istanbul/64, istanbul/99, clique/63, clique/64 all override the "eth" subprotocol.
func isLegacyProtocol(name string, version uint) bool {
	// protocols that override "eth" subprotocol and run only the quorum subprotocol.
	quorumLegacyProtocols := map[string][]uint{"istanbul": {64, 99}, "clique": {63, 64}}
	for lpName, lpVersions := range quorumLegacyProtocols {
		if lpName == name {
			for _, v := range lpVersions {
				if v == version {
					return true
				}
			}
		}
	}
	return false
}

// Used to send consensus subprotocol messages from an "eth" peer, e.g.  "istanbul/100" subprotocol messages.
func (p *peer) SendConsensus(msgcode uint64, data interface{}) error {
	if p.consensusRw == nil {
		return nil
	}
	return p2p.Send(p.consensusRw, msgcode, data)
}

// SendQBFTConsensus is used to send consensus subprotocol messages from an "eth" peer without encoding the payload
func (p *peer) SendQBFTConsensus(msgcode uint64, payload []byte) error {
	if p.consensusRw == nil {
		return nil
	}
	return p2p.SendWithNoEncoding(p.consensusRw, msgcode, payload)
}

func (p *peer) addConsensusProtoRW(rw p2p.MsgReadWriter) *peer {
	p.consensusRw = rw
	return p
}
