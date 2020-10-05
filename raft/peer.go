package raft

import (
	"bytes"
	"fmt"
	"log"
	"net"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
)

// Serializable information about a Peer. Sufficient to build `etcdRaft.Peer`
// or `enode.Node`.
// As NodeId is mainly used to derive the `ecdsa.pubkey` to build `enode.Node` it is kept as [64]byte instead of ID [32]byte used by `enode.Node`.
type Address struct {
	//RaftId   uint64        `json:"raftId"`
	RaftId   uint16        `json:"raftId"`
	NodeId   enode.EnodeID `json:"nodeId"`
	Ip       net.IP        `json:"-"`
	P2pPort  enr.TCP       `json:"p2pPort"`
	RaftPort enr.RaftPort  `json:"raftPort"`

	Hostname string `json:"hostname"`

	// Ignore additional fields (for forward compatibility).
	Rest []rlp.RawValue `json:"-" rlp:"tail"`
}

// We use this as a wrapper when displaying the Address in Javascript, as a Number in Javascript cannot not represent a uint64.
// To fix the JS rendering issue, a string is used instead of an uint64 for the raftId in Javascript.
// https://2ality.com/2012/04/number-encoding.html#:~:text=JavaScript%20numbers&text=JavaScript%20uses%20binary64%20or%20double,binary%20format%2C%20in%2064%20bits.
// javascript: As the former name indicates, numbers are stored in a binary format, in 64 bits. These bits are allotted as follows:
// The fraction occupies bits 0 to 51, the exponent occupies bits 52 to 62, the sign occupies bit 63.
type AddressJS struct {
	RaftId   string        `json:"raftId"`
	NodeId   enode.EnodeID `json:"nodeId"`
	Ip       net.IP        `json:"-"`
	P2pPort  enr.TCP       `json:"p2pPort"`
	RaftPort enr.RaftPort  `json:"raftPort"`

	Hostname string `json:"hostname"`

	// Ignore additional fields (for forward compatibility).
	Rest []rlp.RawValue `json:"-" rlp:"tail"`
}

func newAddressJS(a *Address) *AddressJS {
	return &AddressJS{
		RaftId:   fmt.Sprintf("%8d", a.RaftId),
		NodeId:   a.NodeId,
		Ip:       a.Ip,
		P2pPort:  a.P2pPort,
		RaftPort: a.RaftPort,
		Hostname: a.Hostname,
	}
}

// ClusterInfo is used to display the cluster information in Javascript,
// use the AddressJS instead of Address to display the raftId properly, raftId in Javascipt should be represented as
// a string, as uint64 (go) and Number (js) are not compatible.
type ClusterInfo struct {
	AddressJS
	Role       string `json:"role"`
	NodeActive bool   `json:"nodeActive"`
}

func newAddress(raftId uint16, raftPort int, node *enode.Node, useDns bool) *Address {
	// derive 64 byte nodeID from 128 byte enodeID
	id, err := enode.RaftHexID(node.EnodeID())
	if err != nil {
		panic(err)
	}
	if useDns && node.Host() != "" {
		return &Address{
			RaftId:   raftId,
			NodeId:   id,
			Ip:       nil,
			P2pPort:  enr.TCP(node.TCP()),
			RaftPort: enr.RaftPort(raftPort),
			Hostname: node.Host(),
		}
	}
	return &Address{
		RaftId:   raftId,
		NodeId:   id,
		Ip:       nil,
		P2pPort:  enr.TCP(node.TCP()),
		RaftPort: enr.RaftPort(raftPort),
		Hostname: node.IP().String(),
	}
}

// A peer that we're connected to via both raft's http transport, and ethereum p2p
type Peer struct {
	address *Address    // For raft transport
	p2pNode *enode.Node // For ethereum transport
}

// RLP Address encoding, for transport over raft and storage in LevelDB.
func (addr *Address) toBytes() []byte {
	var toEncode interface{}

	// need to check if addr.Hostname is hostname/ip
	if ip := net.ParseIP(addr.Hostname); ip == nil {
		toEncode = addr
	} else {
		toEncode = []interface{}{addr.RaftId, addr.NodeId, ip, addr.P2pPort, addr.RaftPort}
	}

	buffer, err := rlp.EncodeToBytes(toEncode)
	if err != nil {
		panic(fmt.Sprintf("error: failed to RLP-encode Address: %s", err.Error()))
	}
	return buffer
}

func bytesToAddress(input []byte) *Address {
	// try the new format first
	addr := new(Address)
	streamNew := rlp.NewStream(bytes.NewReader(input), 0)
	if err := streamNew.Decode(addr); err == nil {
		return addr
	} else {
		log.Println(fmt.Sprintf("Libby [streamNew] had error decoding add error [%v]", err))
	}
	log.Println("Libby made it passed the new stream")

	// else try the old format
	var temp struct {
		RaftId   uint16
		NodeId   enode.EnodeID
		Ip       net.IP
		P2pPort  enr.TCP
		RaftPort enr.RaftPort
	}

	streamOld := rlp.NewStream(bytes.NewReader(input), 0)
	if err := streamOld.Decode(&temp); err != nil {
		log.Fatalf("failed to RLP-decode Address: %v", err)
	} else {
		log.Println(fmt.Sprintf("Libby [streamOld] had error decoding add error [%v]", err))
	}

	return &Address{
		RaftId:   temp.RaftId,
		NodeId:   temp.NodeId,
		Ip:       nil,
		P2pPort:  temp.P2pPort,
		RaftPort: temp.RaftPort,
		Hostname: temp.Ip.String(),
	}
}
