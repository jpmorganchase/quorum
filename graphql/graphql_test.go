// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package graphql

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/mps"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/multitenancy"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/private"
	"github.com/ethereum/go-ethereum/private/engine"
	"github.com/ethereum/go-ethereum/private/engine/notinuse"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jpmorganchase/quorum-security-plugin-sdk-go/proto"
	"github.com/stretchr/testify/assert"
)

func TestBuildSchema(t *testing.T) {
	stack, err := node.New(&node.DefaultConfig)
	if err != nil {
		t.Fatalf("could not create new node: %v", err)
	}
	// Make sure the schema can be parsed and matched up to the object model.
	if err := newHandler(stack, nil, []string{}, []string{}); err != nil {
		t.Errorf("Could not construct GraphQL handler: %v", err)
	}
}

// Tests that a graphQL request is successfully handled when graphql is enabled on the specified endpoint
func TestGraphQLHTTPOnSamePort_GQLRequest_Successful(t *testing.T) {
	stack := createNode(t, true)
	defer stack.Close()
	// start node
	if err := stack.Start(); err != nil {
		t.Fatalf("could not start node: %v", err)
	}
	// create http request
	body := strings.NewReader("{\"query\": \"{block{number}}\",\"variables\": null}")
	gqlReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/graphql", "127.0.0.1:9393"), body)
	if err != nil {
		t.Error("could not issue new http request ", err)
	}
	gqlReq.Header.Set("Content-Type", "application/json")
	// read from response
	resp := doHTTPRequest(t, gqlReq)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read from response body: %v", err)
	}
	expected := "{\"data\":{\"block\":{\"number\":\"0x0\"}}}"
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, expected, string(bodyBytes))
}

// Tests that a graphQL request is not handled successfully when graphql is not enabled on the specified endpoint
func TestGraphQLHTTPOnSamePort_GQLRequest_Unsuccessful(t *testing.T) {
	stack := createNode(t, false)
	defer stack.Close()
	if err := stack.Start(); err != nil {
		t.Fatalf("could not start node: %v", err)
	}

	// create http request
	body := strings.NewReader("{\"query\": \"{block{number}}\",\"variables\": null}")
	gqlReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/graphql", "127.0.0.1:9393"), body)
	if err != nil {
		t.Error("could not issue new http request ", err)
	}
	gqlReq.Header.Set("Content-Type", "application/json")
	// read from response
	resp := doHTTPRequest(t, gqlReq)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read from response body: %v", err)
	}
	// make sure the request is not handled successfully
	assert.Equal(t, 404, resp.StatusCode)
	assert.Equal(t, "404 page not found\n", string(bodyBytes))
}

// Tests that 400 is returned when an invalid RPC request is made.
func TestGraphQL_BadRequest(t *testing.T) {
	stack := createNode(t, true)
	defer stack.Close()
	// start node
	if err := stack.Start(); err != nil {
		t.Fatalf("could not start node: %v", err)
	}
	// create http request
	body := strings.NewReader("{\"query\": \"{bleh{number}}\",\"variables\": null}")
	gqlReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/graphql", "127.0.0.1:9393"), body)
	if err != nil {
		t.Error("could not issue new http request ", err)
	}
	gqlReq.Header.Set("Content-Type", "application/json")
	// read from response
	resp := doHTTPRequest(t, gqlReq)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read from response body: %v", err)
	}
	expected := "{\"errors\":[{\"message\":\"Cannot query field \\\"bleh\\\" on type \\\"Query\\\".\",\"locations\":[{\"line\":1,\"column\":2}]}]}"
	assert.Equal(t, expected, string(bodyBytes))
	assert.Equal(t, 400, resp.StatusCode)
}

func createNode(t *testing.T, gqlEnabled bool) *node.Node {
	stack, err := node.New(&node.Config{
		HTTPHost: "127.0.0.1",
		HTTPPort: 9393,
		WSHost:   "127.0.0.1",
		WSPort:   9393,
	})
	if err != nil {
		t.Fatalf("could not create node: %v", err)
	}
	if !gqlEnabled {
		return stack
	}

	createGQLService(t, stack, "127.0.0.1:9393")

	return stack
}

func createGQLService(t *testing.T, stack *node.Node, endpoint string) {
	// create backend (use a config which is light on mem consumption)
	ethConf := &eth.Config{
		Genesis: core.DeveloperGenesisBlock(15, common.Address{}),
		Miner: miner.Config{
			Etherbase: common.HexToAddress("0xaabb"),
		},
		Ethash: ethash.Config{
			PowMode: ethash.ModeTest,
		},
		NetworkId:               1337,
		TrieCleanCache:          5,
		TrieCleanCacheJournal:   "triecache",
		TrieCleanCacheRejournal: 60 * time.Minute,
		TrieDirtyCache:          5,
		TrieTimeout:             60 * time.Minute,
		SnapshotCache:           5,
	}
	ethBackend, err := eth.New(stack, ethConf)
	if err != nil {
		t.Fatalf("could not create eth backend: %v", err)
	}

	// create gql service
	err = New(stack, ethBackend.APIBackend, []string{}, []string{})
	if err != nil {
		t.Fatalf("could not create graphql service: %v", err)
	}
}

func doHTTPRequest(t *testing.T, req *http.Request) *http.Response {
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("could not issue a GET request to the given endpoint", err)

	}
	return resp
}

// Quorum
// Test Quorum specific GraphQL schema for private transaction
func TestQuorumSchema(t *testing.T) {
	saved := private.P
	defer func() {
		private.P = saved
	}()
	arbitraryPayloadHash := common.BytesToEncryptedPayloadHash([]byte("arbitrary key"))
	private.P = &StubPrivateTransactionManager{
		responses: map[common.EncryptedPayloadHash][]interface{}{
			arbitraryPayloadHash: {
				[]byte("private payload"), // equals to 0x70726976617465207061796c6f6164 after converting to bytes
				nil,
			},
		},
	}
	// Test private transaction
	privateTx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), arbitraryPayloadHash.Bytes())
	privateTx.SetPrivate()
	privateTxQuery := &Transaction{tx: privateTx, backend: &StubBackend{}}
	isPrivate, err := privateTxQuery.IsPrivate(context.Background())
	if err != nil {
		t.Fatalf("Expect no error: %v", err)
	}
	if !*isPrivate {
		t.Fatalf("Expect isPrivate to be true for private TX")
	}
	privateInputData, err := privateTxQuery.PrivateInputData(context.Background())
	if err != nil {
		t.Fatalf("Expect no error: %v", err)
	}
	if privateInputData.String() != "0x70726976617465207061796c6f6164" {
		t.Fatalf("Expect privateInputData to be: \"0x70726976617465207061796c6f6164\" for private TX, actual: %v", privateInputData.String())
	}
	// Test public transaction
	publicTx := types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, big.NewInt(0), []byte("key"))
	publicTxQuery := &Transaction{tx: publicTx}
	isPrivate, err = publicTxQuery.IsPrivate(context.Background())
	if err != nil {
		t.Fatalf("Expect no error: %v", err)
	}
	if *isPrivate {
		t.Fatalf("Expect isPrivate to be false for public TX")
	}
	privateInputData, err = publicTxQuery.PrivateInputData(context.Background())
	if err != nil {
		t.Fatalf("Expect no error: %v", err)
	}
	if privateInputData.String() != "0x" {
		t.Fatalf("Expect privateInputData to be: \"0x\" for public TX, actual: %v", privateInputData.String())
	}
}

type StubPrivateTransactionManager struct {
	notinuse.PrivateTransactionManager
	responses map[common.EncryptedPayloadHash][]interface{}
}

func (spm *StubPrivateTransactionManager) HasFeature(f engine.PrivateTransactionManagerFeature) bool {
	return true
}

func (spm *StubPrivateTransactionManager) Receive(txHash common.EncryptedPayloadHash) (string, []string, []byte, *engine.ExtraMetadata, error) {
	res := spm.responses[txHash]
	if err, ok := res[1].(error); ok {
		return "", nil, nil, nil, err
	}
	if ret, ok := res[0].([]byte); ok {
		return "", nil, ret, &engine.ExtraMetadata{
			PrivacyFlag: engine.PrivacyFlagStandardPrivate,
		}, nil
	}
	return "", nil, nil, nil, nil
}

func (spm *StubPrivateTransactionManager) ReceiveRaw(hash common.EncryptedPayloadHash) ([]byte, string, *engine.ExtraMetadata, error) {
	_, sender, data, metadata, err := spm.Receive(hash)
	return data, sender[0], metadata, err
}

type StubBackend struct{}

func (sb *StubBackend) CurrentHeader() *types.Header {
	panic("implement me")
}

func (sb *StubBackend) Engine() consensus.Engine {
	panic("implement me")
}

func (sb *StubBackend) SupportsMultitenancy(rpcCtx context.Context) (*proto.PreAuthenticatedAuthenticationToken, bool) {
	panic("implement me")
}

func (sb *StubBackend) AccountExtraDataStateGetterByNumber(context.Context, rpc.BlockNumber) (vm.AccountExtraDataStateGetter, error) {
	panic("implement me")
}

func (sb *StubBackend) IsAuthorized(authToken *proto.PreAuthenticatedAuthenticationToken, attributes ...*multitenancy.PrivateStateSecurityAttribute) (bool, error) {
	panic("implement me")
}

func (sb *StubBackend) GetEVM(ctx context.Context, msg core.Message, state vm.MinimalApiState, header *types.Header) (*vm.EVM, func() error, error) {
	panic("implement me")
}

func (sb *StubBackend) CurrentBlock() *types.Block {
	panic("implement me")
}

func (sb *StubBackend) Downloader() *downloader.Downloader {
	panic("implement me")
}

func (sb *StubBackend) ProtocolVersion() int {
	panic("implement me")
}

func (sb *StubBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	panic("implement me")
}

func (sb *StubBackend) ChainDb() ethdb.Database {
	panic("implement me")
}

func (sb *StubBackend) EventMux() *event.TypeMux {
	panic("implement me")
}

func (sb *StubBackend) AccountManager() *accounts.Manager {
	panic("implement me")
}

func (sb *StubBackend) ExtRPCEnabled() bool {
	panic("implement me")
}

func (sb *StubBackend) CallTimeOut() time.Duration {
	panic("implement me")
}

func (sb *StubBackend) RPCTxFeeCap() float64 {
	panic("implement me")
}

func (sb *StubBackend) RPCGasCap() uint64 {
	panic("implement me")
}

func (sb *StubBackend) SetHead(number uint64) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (vm.MinimalApiState, *types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.MinimalApiState, *types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	panic("implement me")
}

func (sb *StubBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	panic("implement me")
}

func (sb *StubBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	panic("implement me")
}

func (sb *StubBackend) GetPoolTransactions() (types.Transactions, error) {
	panic("implement me")
}

func (sb *StubBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	panic("implement me")
}

func (sb *StubBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	panic("implement me")
}

func (sb *StubBackend) Stats() (pending int, queued int) {
	panic("implement me")
}

func (sb *StubBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	panic("implement me")
}

func (sb *StubBackend) SubscribeNewTxsEvent(chan<- core.NewTxsEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) BloomStatus() (uint64, uint64) {
	panic("implement me")
}

func (sb *StubBackend) GetLogs(ctx context.Context, blockHash common.Hash) ([][]*types.Log, error) {
	panic("implement me")
}

func (sb *StubBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	panic("implement me")
}

func (sb *StubBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) ChainConfig() *params.ChainConfig {
	panic("implement me")
}

func (sb *StubBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) PSMR() mps.PrivateStateMetadataResolver {
	return &StubPSMR{}
}

type StubPSMR struct {
}

func (psmr *StubPSMR) ResolveForManagedParty(managedParty string) (*mps.PrivateStateMetadata, error) {
	panic("implement me")
}
func (psmr *StubPSMR) ResolveForUserContext(ctx context.Context) (*mps.PrivateStateMetadata, error) {
	return mps.DefaultPrivateStateMetadata, nil
}
func (psmr *StubPSMR) PSIs() []types.PrivateStateIdentifier {
	panic("implement me")
}
func (psmr *StubPSMR) NotIncludeAny(psm *mps.PrivateStateMetadata, managedParties ...string) bool {
	return false
}
