// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"crypto/rand"
	"crypto/tls"
	"flag"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/params"
	testifyassert "github.com/stretchr/testify/assert"
	"gopkg.in/urfave/cli.v1"
)

const (
	ipcAPIs  = "admin:1.0 debug:1.0 eth:1.0 istanbul:1.0 miner:1.0 net:1.0 personal:1.0 rpc:1.0 txpool:1.0 web3:1.0"
	httpAPIs = "admin:1.0 eth:1.0 net:1.0 rpc:1.0 web3:1.0"
	nodeKey  = "b68c0338aa4b266bf38ebe84c6199ae9fac8b29f32998b3ed2fbeafebe8d65c9"
)

var genesis = `{
    "config": {
        "chainId": 2017,
        "homesteadBlock": 1,
        "eip150Block": 2,
        "eip150Hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
        "eip155Block": 3,
        "eip158Block": 3,
        "istanbul": {
            "epoch": 30000,
            "policy": 0
        }
    },
    "nonce": "0x0",
    "timestamp": "0x0",
    "gasLimit": "0x47b760",
    "difficulty": "0x1",
    "mixHash": "0x63746963616c2062797a616e74696e65206661756c7420746f6c6572616e6365",
    "coinbase": "0x0000000000000000000000000000000000000000",
    "alloc": {
        "491937757d1b26e29c507b8d4c0b233c2747e68d": {
            "balance": "0x446c3b15f9926687d2c40534fdb564000000000000"
        }
    },
    "number": "0x0",
    "gasUsed": "0x0",
    "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000"
}
`

// spawns geth with the given command line args, using a set of flags to minimise
// memory and disk IO. If the args don't set --datadir, the
// child g gets a temporary data directory.
func runMinimalGeth(t *testing.T, args ...string) *testgeth {
	// --ropsten to make the 'writing genesis to disk' faster (no accounts)
	// --networkid=1337 to avoid cache bump
	// --syncmode=full to avoid allocating fast sync bloom
	allArgs := []string{ /*"--ropsten",*/ "--nousb", "--networkid", "1337", "--syncmode=full", "--port", "0",
		"--nat", "none", "--nodiscover", "--maxpeers", "0", "--cache", "64"}
	return runGeth(t, append(allArgs, args...)...)
}

// Tests that a node embedded within a console can be started up properly and
// then terminated by closing the input stream.
func TestConsoleWelcome(t *testing.T) {
	defer SetResetPrivateConfig("ignore")()
	coinbase := "0x491937757d1b26e29c507b8d4c0b233c2747e68d"

	datadir := setupIstanbul(t)
	defer os.RemoveAll(datadir)

	// Start a geth console, make sure it's cleaned up and terminate the console
	geth := runMinimalGeth(t, "--datadir", datadir, "--etherbase", coinbase, "console")

	// Gather all the infos the welcome message needs to contain
	geth.SetTemplateFunc("goos", func() string { return runtime.GOOS })
	geth.SetTemplateFunc("goarch", func() string { return runtime.GOARCH })
	geth.SetTemplateFunc("gover", runtime.Version)
	geth.SetTemplateFunc("gethver", func() string { return params.VersionWithMeta })
	geth.SetTemplateFunc("quorumver", func() string { return params.QuorumVersion })
	geth.SetTemplateFunc("niltime", func() string {
		return time.Unix(0, 0).Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)")
	})
	geth.SetTemplateFunc("apis", func() string { return ipcAPIs })

	// Verify the actual welcome message to the required template
	geth.Expect(`
Welcome to the Geth JavaScript console!

instance: Geth/v{{gethver}}(quorum-v{{quorumver}})/{{goos}}-{{goarch}}/{{gover}}
coinbase: {{.Etherbase}}
at block: 0 ({{niltime}})
 datadir: {{.Datadir}}
 modules: {{apis}}

To exit, press ctrl-d
> {{.InputLine "exit"}}
`)
	geth.ExpectExit()
}

// Tests that a console can be attached to a running node via various means.
func TestAttachWelcome(t *testing.T) {
	var (
		ipc      string
		httpPort string
		wsPort   string
	)
	defer SetResetPrivateConfig("ignore")()
	// Configure the instance for IPC attachment
	coinbase := "0x491937757d1b26e29c507b8d4c0b233c2747e68d"

	datadir := setupIstanbul(t)
	defer os.RemoveAll(datadir)
	if runtime.GOOS == "windows" {
		ipc = `\\.\pipe\geth` + strconv.Itoa(trulyRandInt(100000, 999999))
	} else {
		ipc = filepath.Join(datadir, "geth.ipc")
	}
	// And HTTP + WS attachment
	p := trulyRandInt(1024, 65533) // Yeah, sometimes this will fail, sorry :P
	httpPort = strconv.Itoa(p)
	wsPort = strconv.Itoa(p + 1)
	/*geth := runGeth(t,
	"--datadir", datadir, "--port", "0", "--maxpeers", "0", "--nodiscover", "--nat", "none",
	"--etherbase", coinbase,
	"--http", "--http.port", httpPort, "--rpcapi", "admin,eth,net,web3",
	"--ws", "--ws.port", wsPort, "--wsapi", "admin,eth,net,web3")*/
	geth := runMinimalGeth(t, "--datadir", datadir, "--etherbase", coinbase,
		"--ipcpath", ipc,
		"--http", "--http.port", httpPort, "--rpcapi", "admin,eth,net,web3",
		"--ws", "--ws.port", wsPort, "--wsapi", "admin,eth,net,web3")
	t.Run("ipc", func(t *testing.T) {
		waitForEndpoint(t, ipc, 3*time.Second)
		testAttachWelcome(t, geth, "ipc:"+ipc, ipcAPIs)
	})
	t.Run("http", func(t *testing.T) {
		endpoint := "http://127.0.0.1:" + httpPort
		waitForEndpoint(t, endpoint, 3*time.Second)
		testAttachWelcome(t, geth, endpoint, httpAPIs)
	})
	t.Run("ws", func(t *testing.T) {
		endpoint := "ws://127.0.0.1:" + wsPort
		waitForEndpoint(t, endpoint, 3*time.Second)
		testAttachWelcome(t, geth, endpoint, httpAPIs)
	})
}

func TestHTTPAttachWelcome(t *testing.T) {
	defer SetResetPrivateConfig("ignore")()
	coinbase := "0x491937757d1b26e29c507b8d4c0b233c2747e68d"
	port := strconv.Itoa(trulyRandInt(1024, 65536)) // Yeah, sometimes this will fail, sorry :P

	datadir := setupIstanbul(t)
	defer os.RemoveAll(datadir)

	geth := runGeth(t,
		"--datadir", datadir, "--port", "0", "--maxpeers", "0", "--nodiscover", "--nat", "none",
		"--etherbase", coinbase, "--http", "--http.port", port, "--rpcapi", "admin,eth,net,web3")

	endpoint := "http://127.0.0.1:" + port
	waitForEndpoint(t, endpoint, 3*time.Second)
	testAttachWelcome(t, geth, endpoint, httpAPIs)
}

func TestWSAttachWelcome(t *testing.T) {
	defer SetResetPrivateConfig("ignore")()
	coinbase := "0x491937757d1b26e29c507b8d4c0b233c2747e68d"
	port := strconv.Itoa(trulyRandInt(1024, 65536)) // Yeah, sometimes this will fail, sorry :P

	datadir := setupIstanbul(t)
	defer os.RemoveAll(datadir)

	geth := runGeth(t,
		"--datadir", datadir, "--port", "0", "--maxpeers", "0", "--nodiscover", "--nat", "none",
		"--etherbase", coinbase, "--ws", "--ws.port", port, "--wsapi", "admin,eth,net,web3")

	endpoint := "ws://127.0.0.1:" + port
	waitForEndpoint(t, endpoint, 3*time.Second)
	testAttachWelcome(t, geth, endpoint, httpAPIs)
}

func testAttachWelcome(t *testing.T, geth *testgeth, endpoint, apis string) {
	// Attach to a running geth note and terminate immediately
	attach := runGeth(t, "attach", endpoint)
	defer attach.ExpectExit()
	attach.CloseStdin()

	// Gather all the infos the welcome message needs to contain
	attach.SetTemplateFunc("goos", func() string { return runtime.GOOS })
	attach.SetTemplateFunc("goarch", func() string { return runtime.GOARCH })
	attach.SetTemplateFunc("gover", runtime.Version)
	attach.SetTemplateFunc("gethver", func() string { return params.VersionWithMeta })
	attach.SetTemplateFunc("quorumver", func() string { return params.QuorumVersion })
	attach.SetTemplateFunc("etherbase", func() string { return geth.Etherbase })
	attach.SetTemplateFunc("niltime", func() string {
		return time.Unix(0, 0).Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)")
	})
	attach.SetTemplateFunc("ipc", func() bool {
		return strings.HasPrefix(endpoint, "ipc") || strings.Contains(apis, "admin")
	})
	attach.SetTemplateFunc("datadir", func() string { return geth.Datadir })
	attach.SetTemplateFunc("apis", func() string { return apis })

	// Verify the actual welcome message to the required template
	attach.Expect(`
Welcome to the Geth JavaScript console!

instance: Geth/v{{gethver}}(quorum-v{{quorumver}})/{{goos}}-{{goarch}}/{{gover}}
coinbase: {{etherbase}}
at block: 0 ({{niltime}}){{if ipc}}
 datadir: {{datadir}}{{end}}
 modules: {{apis}}

To exit, press ctrl-d
> {{.InputLine "exit" }}
`)
	attach.ExpectExit()
}

// trulyRandInt generates a crypto random integer used by the console tests to
// not clash network ports with other tests running cocurrently.
func trulyRandInt(lo, hi int) int {
	num, _ := rand.Int(rand.Reader, big.NewInt(int64(hi-lo)))
	return int(num.Int64()) + lo
}

// setupIstanbul creates a temporary directory and copies nodekey and genesis.json.
// It initializes istanbul by calling geth init
func setupIstanbul(t *testing.T) string {
	datadir := tmpdir(t)
	gethPath := filepath.Join(datadir, "geth")
	os.Mkdir(gethPath, 0700)

	// Initialize the data directory with the custom genesis block
	json := filepath.Join(datadir, "genesis.json")
	if err := ioutil.WriteFile(json, []byte(genesis), 0600); err != nil {
		t.Fatalf("failed to write genesis file: %v", err)
	}

	nodeKeyFile := filepath.Join(gethPath, "nodekey")
	if err := ioutil.WriteFile(nodeKeyFile, []byte(nodeKey), 0600); err != nil {
		t.Fatalf("failed to write nodekey file: %v", err)
	}

	runGeth(t, "--datadir", datadir, "init", json).WaitExit()

	return datadir
}

func TestReadTLSClientConfig_whenCustomizeTLSCipherSuites(t *testing.T) {
	assert := testifyassert.New(t)

	flagSet := new(flag.FlagSet)
	flagSet.Bool(utils.RPCClientTLSInsecureSkipVerify.Name, true, "")
	flagSet.String(utils.RPCClientTLSCipherSuites.Name, "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,  TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384", "")
	ctx := cli.NewContext(nil, flagSet, nil)

	tlsConf, ok, err := readTLSClientConfig("https://arbitraryendpoint", ctx)

	assert.NoError(err)
	assert.True(ok, "has custom TLS client configuration")
	assert.True(tlsConf.InsecureSkipVerify)
	assert.Len(tlsConf.CipherSuites, 2)
	assert.Contains(tlsConf.CipherSuites, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384)
	assert.Contains(tlsConf.CipherSuites, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384)
}

func TestReadTLSClientConfig_whenTypicalTLS(t *testing.T) {
	assert := testifyassert.New(t)

	flagSet := new(flag.FlagSet)
	ctx := cli.NewContext(nil, flagSet, nil)

	tlsConf, ok, err := readTLSClientConfig("https://arbitraryendpoint", ctx)

	assert.NoError(err)
	assert.False(ok, "no custom TLS client configuration")
	assert.Nil(tlsConf, "no custom TLS config is set")
}

func TestReadTLSClientConfig_whenTLSInsecureFlagSet(t *testing.T) {
	assert := testifyassert.New(t)

	flagSet := new(flag.FlagSet)
	flagSet.Bool(utils.RPCClientTLSInsecureSkipVerify.Name, true, "")
	ctx := cli.NewContext(nil, flagSet, nil)

	tlsConf, ok, err := readTLSClientConfig("https://arbitraryendpoint", ctx)

	assert.NoError(err)
	assert.True(ok, "has custom TLS client configuration")
	assert.True(tlsConf.InsecureSkipVerify)
	assert.Len(tlsConf.CipherSuites, 0)
}

func SetResetPrivateConfig(value string) func() {
	existingValue := os.Getenv("PRIVATE_CONFIG")
	os.Setenv("PRIVATE_CONFIG", value)
	return func() {
		os.Setenv("PRIVATE_CONFIG", existingValue)
	}
}
