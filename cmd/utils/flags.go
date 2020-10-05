// Copyright 2015 The go-ethereum Authors
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

// Package utils contains internal helper functions for go-ethereum commands.
package utils

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulBackend "github.com/ethereum/go-ethereum/consensus/istanbul/backend"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/dashboard"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethstats"
	"github.com/ethereum/go-ethereum/extension"
	"github.com/ethereum/go-ethereum/graphql"
	"github.com/ethereum/go-ethereum/les"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/metrics/influxdb"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/discv5"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/p2p/netutil"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/permission"
	"github.com/ethereum/go-ethereum/plugin"
	"github.com/ethereum/go-ethereum/private"
	"github.com/ethereum/go-ethereum/raft"
	"github.com/ethereum/go-ethereum/rpc"
	whisper "github.com/ethereum/go-ethereum/whisper/whisperv6"
	pcsclite "github.com/gballet/go-libpcsclite"
	"gopkg.in/urfave/cli.v1"
)

var (
	CommandHelpTemplate = `{{.cmd.Name}}{{if .cmd.Subcommands}} command{{end}}{{if .cmd.Flags}} [command options]{{end}} [arguments...]
{{if .cmd.Description}}{{.cmd.Description}}
{{end}}{{if .cmd.Subcommands}}
SUBCOMMANDS:
	{{range .cmd.Subcommands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
	{{end}}{{end}}{{if .categorizedFlags}}
{{range $idx, $categorized := .categorizedFlags}}{{$categorized.Name}} OPTIONS:
{{range $categorized.Flags}}{{"\t"}}{{.}}
{{end}}
{{end}}{{end}}`
)

func init() {
	cli.AppHelpTemplate = `{{.Name}} {{if .Flags}}[global options] {{end}}command{{if .Flags}} [command options]{{end}} [arguments...]

VERSION:
   {{.Version}}

COMMANDS:
   {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
   {{end}}{{if .Flags}}
GLOBAL OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}{{end}}
`
	cli.CommandHelpTemplate = CommandHelpTemplate
	cli.HelpPrinter = printHelp
}

// NewApp creates an app with sane defaults.
func NewApp(gitCommit, gitDate, usage string) *cli.App {
	app := cli.NewApp()
	app.Name = filepath.Base(os.Args[0])
	app.Author = ""
	app.Email = ""
	app.Version = params.VersionWithCommit(gitCommit, gitDate)
	app.Usage = usage
	return app
}

func printHelp(out io.Writer, templ string, data interface{}) {
	funcMap := template.FuncMap{"join": strings.Join}
	t := template.Must(template.New("help").Funcs(funcMap).Parse(templ))
	w := tabwriter.NewWriter(out, 38, 8, 2, ' ', 0)
	err := t.Execute(w, data)
	if err != nil {
		panic(err)
	}
	w.Flush()
}

// These are all the command line flags we support.
// If you add to this list, please remember to include the
// flag in the appropriate command definition.
//
// The flags are defined here so their names and help texts
// are the same for all commands.

var (
	// General settings
	DataDirFlag = DirectoryFlag{
		Name:  "datadir",
		Usage: "Data directory for the databases and keystore",
		Value: DirectoryString(node.DefaultDataDir()),
	}
	AncientFlag = DirectoryFlag{
		Name:  "datadir.ancient",
		Usage: "Data directory for ancient chain segments (default = inside chaindata)",
	}
	KeyStoreDirFlag = DirectoryFlag{
		Name:  "keystore",
		Usage: "Directory for the keystore (default = inside the datadir)",
	}
	NoUSBFlag = cli.BoolFlag{
		Name:  "nousb",
		Usage: "Disables monitoring for and managing USB hardware wallets",
	}
	SmartCardDaemonPathFlag = cli.StringFlag{
		Name:  "pcscdpath",
		Usage: "Path to the smartcard daemon (pcscd) socket file",
		Value: pcsclite.PCSCDSockName,
	}
	NetworkIdFlag = cli.Uint64Flag{
		Name:  "networkid",
		Usage: "Network identifier (integer, 1=Frontier, 2=Morden (disused), 3=Ropsten, 4=Rinkeby)",
		Value: eth.DefaultConfig.NetworkId,
	}
	TestnetFlag = cli.BoolFlag{
		Name:  "testnet",
		Usage: "Ropsten network: pre-configured proof-of-work test network",
	}
	RinkebyFlag = cli.BoolFlag{
		Name:  "rinkeby",
		Usage: "Rinkeby network: pre-configured proof-of-authority test network",
	}
	GoerliFlag = cli.BoolFlag{
		Name:  "goerli",
		Usage: "Görli network: pre-configured proof-of-authority test network",
	}
	DeveloperFlag = cli.BoolFlag{
		Name:  "dev",
		Usage: "Ephemeral proof-of-authority network with a pre-funded developer account, mining enabled",
	}
	DeveloperPeriodFlag = cli.IntFlag{
		Name:  "dev.period",
		Usage: "Block period to use in developer mode (0 = mine only if transaction pending)",
	}
	IdentityFlag = cli.StringFlag{
		Name:  "identity",
		Usage: "Custom node name",
	}
	DocRootFlag = DirectoryFlag{
		Name:  "docroot",
		Usage: "Document Root for HTTPClient file scheme",
		Value: DirectoryString(homeDir()),
	}
	ExitWhenSyncedFlag = cli.BoolFlag{
		Name:  "exitwhensynced",
		Usage: "Exits after block synchronisation completes",
	}
	IterativeOutputFlag = cli.BoolFlag{
		Name:  "iterative",
		Usage: "Print streaming JSON iteratively, delimited by newlines",
	}
	ExcludeStorageFlag = cli.BoolFlag{
		Name:  "nostorage",
		Usage: "Exclude storage entries (save db lookups)",
	}
	IncludeIncompletesFlag = cli.BoolFlag{
		Name:  "incompletes",
		Usage: "Include accounts for which we don't have the address (missing preimage)",
	}
	ExcludeCodeFlag = cli.BoolFlag{
		Name:  "nocode",
		Usage: "Exclude contract code (save db lookups)",
	}
	defaultSyncMode = eth.DefaultConfig.SyncMode
	SyncModeFlag    = TextMarshalerFlag{
		Name:  "syncmode",
		Usage: `Blockchain sync mode ("fast", "full", or "light")`,
		Value: &defaultSyncMode,
	}
	GCModeFlag = cli.StringFlag{
		Name:  "gcmode",
		Usage: `Blockchain garbage collection mode ("full", "archive")`,
		Value: "full",
	}
	LightKDFFlag = cli.BoolFlag{
		Name:  "lightkdf",
		Usage: "Reduce key-derivation RAM & CPU usage at some expense of KDF strength",
	}
	WhitelistFlag = cli.StringFlag{
		Name:  "whitelist",
		Usage: "Comma separated block number-to-hash mappings to enforce (<number>=<hash>)",
	}
	OverrideIstanbulFlag = cli.Uint64Flag{
		Name:  "override.istanbul",
		Usage: "Manually specify Istanbul fork-block, overriding the bundled setting",
	}
	// Light server and client settings
	LightLegacyServFlag = cli.IntFlag{ // Deprecated in favor of light.serve, remove in 2021
		Name:  "lightserv",
		Usage: "Maximum percentage of time allowed for serving LES requests (deprecated, use --light.serve)",
		Value: eth.DefaultConfig.LightServ,
	}
	LightServeFlag = cli.IntFlag{
		Name:  "light.serve",
		Usage: "Maximum percentage of time allowed for serving LES requests (multi-threaded processing allows values over 100)",
		Value: eth.DefaultConfig.LightServ,
	}
	LightIngressFlag = cli.IntFlag{
		Name:  "light.ingress",
		Usage: "Incoming bandwidth limit for serving light clients (kilobytes/sec, 0 = unlimited)",
		Value: eth.DefaultConfig.LightIngress,
	}
	LightEgressFlag = cli.IntFlag{
		Name:  "light.egress",
		Usage: "Outgoing bandwidth limit for serving light clients (kilobytes/sec, 0 = unlimited)",
		Value: eth.DefaultConfig.LightEgress,
	}
	LightLegacyPeersFlag = cli.IntFlag{ // Deprecated in favor of light.maxpeers, remove in 2021
		Name:  "lightpeers",
		Usage: "Maximum number of light clients to serve, or light servers to attach to  (deprecated, use --light.maxpeers)",
		Value: eth.DefaultConfig.LightPeers,
	}
	LightMaxPeersFlag = cli.IntFlag{
		Name:  "light.maxpeers",
		Usage: "Maximum number of light clients to serve, or light servers to attach to",
		Value: eth.DefaultConfig.LightPeers,
	}
	UltraLightServersFlag = cli.StringFlag{
		Name:  "ulc.servers",
		Usage: "List of trusted ultra-light servers",
		Value: strings.Join(eth.DefaultConfig.UltraLightServers, ","),
	}
	UltraLightFractionFlag = cli.IntFlag{
		Name:  "ulc.fraction",
		Usage: "Minimum % of trusted ultra-light servers required to announce a new head",
		Value: eth.DefaultConfig.UltraLightFraction,
	}
	UltraLightOnlyAnnounceFlag = cli.BoolFlag{
		Name:  "ulc.onlyannounce",
		Usage: "Ultra light server sends announcements only",
	}
	// Dashboard settings
	DashboardEnabledFlag = cli.BoolFlag{
		Name:  "dashboard",
		Usage: "Enable the dashboard",
	}
	DashboardAddrFlag = cli.StringFlag{
		Name:  "dashboard.addr",
		Usage: "Dashboard listening interface",
		Value: dashboard.DefaultConfig.Host,
	}
	DashboardPortFlag = cli.IntFlag{
		Name:  "dashboard.host",
		Usage: "Dashboard listening port",
		Value: dashboard.DefaultConfig.Port,
	}
	DashboardRefreshFlag = cli.DurationFlag{
		Name:  "dashboard.refresh",
		Usage: "Dashboard metrics collection refresh rate",
		Value: dashboard.DefaultConfig.Refresh,
	}
	// Ethash settings
	EthashCacheDirFlag = DirectoryFlag{
		Name:  "ethash.cachedir",
		Usage: "Directory to store the ethash verification caches (default = inside the datadir)",
	}
	EthashCachesInMemoryFlag = cli.IntFlag{
		Name:  "ethash.cachesinmem",
		Usage: "Number of recent ethash caches to keep in memory (16MB each)",
		Value: eth.DefaultConfig.Ethash.CachesInMem,
	}
	EthashCachesOnDiskFlag = cli.IntFlag{
		Name:  "ethash.cachesondisk",
		Usage: "Number of recent ethash caches to keep on disk (16MB each)",
		Value: eth.DefaultConfig.Ethash.CachesOnDisk,
	}
	EthashDatasetDirFlag = DirectoryFlag{
		Name:  "ethash.dagdir",
		Usage: "Directory to store the ethash mining DAGs",
		Value: DirectoryString(eth.DefaultConfig.Ethash.DatasetDir),
	}
	EthashDatasetsInMemoryFlag = cli.IntFlag{
		Name:  "ethash.dagsinmem",
		Usage: "Number of recent ethash mining DAGs to keep in memory (1+GB each)",
		Value: eth.DefaultConfig.Ethash.DatasetsInMem,
	}
	EthashDatasetsOnDiskFlag = cli.IntFlag{
		Name:  "ethash.dagsondisk",
		Usage: "Number of recent ethash mining DAGs to keep on disk (1+GB each)",
		Value: eth.DefaultConfig.Ethash.DatasetsOnDisk,
	}
	// Transaction pool settings
	TxPoolLocalsFlag = cli.StringFlag{
		Name:  "txpool.locals",
		Usage: "Comma separated accounts to treat as locals (no flush, priority inclusion)",
	}
	TxPoolNoLocalsFlag = cli.BoolFlag{
		Name:  "txpool.nolocals",
		Usage: "Disables price exemptions for locally submitted transactions",
	}
	TxPoolJournalFlag = cli.StringFlag{
		Name:  "txpool.journal",
		Usage: "Disk journal for local transaction to survive node restarts",
		Value: core.DefaultTxPoolConfig.Journal,
	}
	TxPoolRejournalFlag = cli.DurationFlag{
		Name:  "txpool.rejournal",
		Usage: "Time interval to regenerate the local transaction journal",
		Value: core.DefaultTxPoolConfig.Rejournal,
	}
	TxPoolPriceLimitFlag = cli.Uint64Flag{
		Name:  "txpool.pricelimit",
		Usage: "Minimum gas price limit to enforce for acceptance into the pool",
		Value: eth.DefaultConfig.TxPool.PriceLimit,
	}
	TxPoolPriceBumpFlag = cli.Uint64Flag{
		Name:  "txpool.pricebump",
		Usage: "Price bump percentage to replace an already existing transaction",
		Value: eth.DefaultConfig.TxPool.PriceBump,
	}
	TxPoolAccountSlotsFlag = cli.Uint64Flag{
		Name:  "txpool.accountslots",
		Usage: "Minimum number of executable transaction slots guaranteed per account",
		Value: eth.DefaultConfig.TxPool.AccountSlots,
	}
	TxPoolGlobalSlotsFlag = cli.Uint64Flag{
		Name:  "txpool.globalslots",
		Usage: "Maximum number of executable transaction slots for all accounts",
		Value: eth.DefaultConfig.TxPool.GlobalSlots,
	}
	TxPoolAccountQueueFlag = cli.Uint64Flag{
		Name:  "txpool.accountqueue",
		Usage: "Maximum number of non-executable transaction slots permitted per account",
		Value: eth.DefaultConfig.TxPool.AccountQueue,
	}
	TxPoolGlobalQueueFlag = cli.Uint64Flag{
		Name:  "txpool.globalqueue",
		Usage: "Maximum number of non-executable transaction slots for all accounts",
		Value: eth.DefaultConfig.TxPool.GlobalQueue,
	}
	TxPoolLifetimeFlag = cli.DurationFlag{
		Name:  "txpool.lifetime",
		Usage: "Maximum amount of time non-executable transaction are queued",
		Value: eth.DefaultConfig.TxPool.Lifetime,
	}
	// Performance tuning settings
	CacheFlag = cli.IntFlag{
		Name:  "cache",
		Usage: "Megabytes of memory allocated to internal caching (default = 4096 mainnet full node, 128 light mode)",
		Value: 1024,
	}
	CacheDatabaseFlag = cli.IntFlag{
		Name:  "cache.database",
		Usage: "Percentage of cache memory allowance to use for database io",
		Value: 50,
	}
	CacheTrieFlag = cli.IntFlag{
		Name:  "cache.trie",
		Usage: "Percentage of cache memory allowance to use for trie caching (default = 25% full mode, 50% archive mode)",
		Value: 25,
	}
	CacheGCFlag = cli.IntFlag{
		Name:  "cache.gc",
		Usage: "Percentage of cache memory allowance to use for trie pruning (default = 25% full mode, 0% archive mode)",
		Value: 25,
	}
	CacheNoPrefetchFlag = cli.BoolFlag{
		Name:  "cache.noprefetch",
		Usage: "Disable heuristic state prefetch during block import (less CPU and disk IO, more time waiting for data)",
	}
	// Miner settings
	MiningEnabledFlag = cli.BoolFlag{
		Name:  "mine",
		Usage: "Enable mining",
	}
	MinerThreadsFlag = cli.IntFlag{
		Name:  "miner.threads",
		Usage: "Number of CPU threads to use for mining",
		Value: 0,
	}
	MinerLegacyThreadsFlag = cli.IntFlag{
		Name:  "minerthreads",
		Usage: "Number of CPU threads to use for mining (deprecated, use --miner.threads)",
		Value: 0,
	}
	MinerNotifyFlag = cli.StringFlag{
		Name:  "miner.notify",
		Usage: "Comma separated HTTP URL list to notify of new work packages",
	}
	MinerGasTargetFlag = cli.Uint64Flag{
		Name:  "miner.gastarget",
		Usage: "Target gas floor for mined blocks",
		Value: eth.DefaultConfig.Miner.GasFloor,
	}
	MinerLegacyGasTargetFlag = cli.Uint64Flag{
		Name:  "targetgaslimit",
		Usage: "Target gas floor for mined blocks (deprecated, use --miner.gastarget)",
		Value: eth.DefaultConfig.Miner.GasFloor,
	}
	MinerGasLimitFlag = cli.Uint64Flag{
		Name:  "miner.gaslimit",
		Usage: "Target gas ceiling for mined blocks",
		Value: eth.DefaultConfig.Miner.GasCeil,
	}
	MinerGasPriceFlag = BigFlag{
		Name:  "miner.gasprice",
		Usage: "Minimum gas price for mining a transaction",
		Value: eth.DefaultConfig.Miner.GasPrice,
	}
	MinerLegacyGasPriceFlag = BigFlag{
		Name:  "gasprice",
		Usage: "Minimum gas price for mining a transaction (deprecated, use --miner.gasprice)",
		Value: eth.DefaultConfig.Miner.GasPrice,
	}
	MinerEtherbaseFlag = cli.StringFlag{
		Name:  "miner.etherbase",
		Usage: "Public address for block mining rewards (default = first account)",
		Value: "0",
	}
	MinerLegacyEtherbaseFlag = cli.StringFlag{
		Name:  "etherbase",
		Usage: "Public address for block mining rewards (default = first account, deprecated, use --miner.etherbase)",
		Value: "0",
	}
	MinerExtraDataFlag = cli.StringFlag{
		Name:  "miner.extradata",
		Usage: "Block extra data set by the miner (default = client version)",
	}
	MinerLegacyExtraDataFlag = cli.StringFlag{
		Name:  "extradata",
		Usage: "Block extra data set by the miner (default = client version, deprecated, use --miner.extradata)",
	}
	MinerRecommitIntervalFlag = cli.DurationFlag{
		Name:  "miner.recommit",
		Usage: "Time interval to recreate the block being mined",
		Value: eth.DefaultConfig.Miner.Recommit,
	}
	MinerNoVerfiyFlag = cli.BoolFlag{
		Name:  "miner.noverify",
		Usage: "Disable remote sealing verification",
	}
	// Account settings
	UnlockedAccountFlag = cli.StringFlag{
		Name:  "unlock",
		Usage: "Comma separated list of accounts to unlock",
		Value: "",
	}
	PasswordFileFlag = cli.StringFlag{
		Name:  "password",
		Usage: "Password file to use for non-interactive password input",
		Value: "",
	}
	ExternalSignerFlag = cli.StringFlag{
		Name:  "signer",
		Usage: "External signer (url or path to ipc file)",
		Value: "",
	}
	VMEnableDebugFlag = cli.BoolFlag{
		Name:  "vmdebug",
		Usage: "Record information useful for VM and contract debugging",
	}
	InsecureUnlockAllowedFlag = cli.BoolFlag{
		Name:  "allow-insecure-unlock",
		Usage: "Allow insecure account unlocking when account-related RPCs are exposed by http",
	}
	RPCGlobalGasCap = cli.Uint64Flag{
		Name:  "rpc.gascap",
		Usage: "Sets a cap on gas that can be used in eth_call/estimateGas",
	}
	// Logging and debug settings
	EthStatsURLFlag = cli.StringFlag{
		Name:  "ethstats",
		Usage: "Reporting URL of a ethstats service (nodename:secret@host:port)",
	}
	FakePoWFlag = cli.BoolFlag{
		Name:  "fakepow",
		Usage: "Disables proof-of-work verification",
	}
	NoCompactionFlag = cli.BoolFlag{
		Name:  "nocompaction",
		Usage: "Disables db compaction after import",
	}
	// RPC Client Settings
	RPCClientToken = cli.StringFlag{
		Name:  "rpcclitoken",
		Usage: "RPC Client access token",
	}
	RPCClientTLSCert = cli.StringFlag{
		Name:  "rpcclitls.cert",
		Usage: "Server's TLS certificate PEM file on connection by client",
	}
	RPCClientTLSCaCert = cli.StringFlag{
		Name:  "rpcclitls.cacert",
		Usage: "CA certificate PEM file for provided server's TLS certificate on connection by client",
	}
	RPCClientTLSCipherSuites = cli.StringFlag{
		Name:  "rpcclitls.ciphersuites",
		Usage: "Customize supported cipher suites when using TLS connection. Value is a comma-separated cipher suite string",
	}
	RPCClientTLSInsecureSkipVerify = cli.BoolFlag{
		Name:  "rpcclitls.insecureskipverify",
		Usage: "Disable verification of server's TLS certificate on connection by client",
	}
	// RPC settings
	IPCDisabledFlag = cli.BoolFlag{
		Name:  "ipcdisable",
		Usage: "Disable the IPC-RPC server",
	}
	IPCPathFlag = DirectoryFlag{
		Name:  "ipcpath",
		Usage: "Filename for IPC socket/pipe within the datadir (explicit paths escape it)",
	}
	RPCEnabledFlag = cli.BoolFlag{
		Name:  "rpc",
		Usage: "Enable the HTTP-RPC server",
	}
	RPCListenAddrFlag = cli.StringFlag{
		Name:  "rpcaddr",
		Usage: "HTTP-RPC server listening interface",
		Value: node.DefaultHTTPHost,
	}
	RPCPortFlag = cli.IntFlag{
		Name:  "rpcport",
		Usage: "HTTP-RPC server listening port",
		Value: node.DefaultHTTPPort,
	}
	RPCCORSDomainFlag = cli.StringFlag{
		Name:  "rpccorsdomain",
		Usage: "Comma separated list of domains from which to accept cross origin requests (browser enforced)",
		Value: "",
	}
	RPCVirtualHostsFlag = cli.StringFlag{
		Name:  "rpcvhosts",
		Usage: "Comma separated list of virtual hostnames from which to accept requests (server enforced). Accepts '*' wildcard.",
		Value: strings.Join(node.DefaultConfig.HTTPVirtualHosts, ","),
	}
	RPCApiFlag = cli.StringFlag{
		Name:  "rpcapi",
		Usage: "API's offered over the HTTP-RPC interface",
		Value: "",
	}
	WSEnabledFlag = cli.BoolFlag{
		Name:  "ws",
		Usage: "Enable the WS-RPC server",
	}
	WSListenAddrFlag = cli.StringFlag{
		Name:  "wsaddr",
		Usage: "WS-RPC server listening interface",
		Value: node.DefaultWSHost,
	}
	WSPortFlag = cli.IntFlag{
		Name:  "wsport",
		Usage: "WS-RPC server listening port",
		Value: node.DefaultWSPort,
	}
	WSApiFlag = cli.StringFlag{
		Name:  "wsapi",
		Usage: "API's offered over the WS-RPC interface",
		Value: "",
	}
	WSAllowedOriginsFlag = cli.StringFlag{
		Name:  "wsorigins",
		Usage: "Origins from which to accept websockets requests",
		Value: "",
	}
	GraphQLEnabledFlag = cli.BoolFlag{
		Name:  "graphql",
		Usage: "Enable the GraphQL server",
	}
	GraphQLListenAddrFlag = cli.StringFlag{
		Name:  "graphql.addr",
		Usage: "GraphQL server listening interface",
		Value: node.DefaultGraphQLHost,
	}
	GraphQLPortFlag = cli.IntFlag{
		Name:  "graphql.port",
		Usage: "GraphQL server listening port",
		Value: node.DefaultGraphQLPort,
	}
	GraphQLCORSDomainFlag = cli.StringFlag{
		Name:  "graphql.corsdomain",
		Usage: "Comma separated list of domains from which to accept cross origin requests (browser enforced)",
		Value: "",
	}
	GraphQLVirtualHostsFlag = cli.StringFlag{
		Name:  "graphql.vhosts",
		Usage: "Comma separated list of virtual hostnames from which to accept requests (server enforced). Accepts '*' wildcard.",
		Value: strings.Join(node.DefaultConfig.GraphQLVirtualHosts, ","),
	}
	ExecFlag = cli.StringFlag{
		Name:  "exec",
		Usage: "Execute JavaScript statement",
	}
	PreloadJSFlag = cli.StringFlag{
		Name:  "preload",
		Usage: "Comma separated list of JavaScript files to preload into the console",
	}

	// Network Settings
	MaxPeersFlag = cli.IntFlag{
		Name:  "maxpeers",
		Usage: "Maximum number of network peers (network disabled if set to 0)",
		Value: node.DefaultConfig.P2P.MaxPeers,
	}
	MaxPendingPeersFlag = cli.IntFlag{
		Name:  "maxpendpeers",
		Usage: "Maximum number of pending connection attempts (defaults used if set to 0)",
		Value: node.DefaultConfig.P2P.MaxPendingPeers,
	}
	ListenPortFlag = cli.IntFlag{
		Name:  "port",
		Usage: "Network listening port",
		Value: 30303,
	}
	BootnodesFlag = cli.StringFlag{
		Name:  "bootnodes",
		Usage: "Comma separated enode URLs for P2P discovery bootstrap (set v4+v5 instead for light servers)",
		Value: "",
	}
	BootnodesV4Flag = cli.StringFlag{
		Name:  "bootnodesv4",
		Usage: "Comma separated enode URLs for P2P v4 discovery bootstrap (light server, full nodes)",
		Value: "",
	}
	BootnodesV5Flag = cli.StringFlag{
		Name:  "bootnodesv5",
		Usage: "Comma separated enode URLs for P2P v5 discovery bootstrap (light server, light nodes)",
		Value: "",
	}
	NodeKeyFileFlag = cli.StringFlag{
		Name:  "nodekey",
		Usage: "P2P node key file",
	}
	NodeKeyHexFlag = cli.StringFlag{
		Name:  "nodekeyhex",
		Usage: "P2P node key as hex (for testing)",
	}
	NATFlag = cli.StringFlag{
		Name:  "nat",
		Usage: "NAT port mapping mechanism (any|none|upnp|pmp|extip:<IP>)",
		Value: "any",
	}
	NoDiscoverFlag = cli.BoolFlag{
		Name:  "nodiscover",
		Usage: "Disables the peer discovery mechanism (manual peer addition)",
	}
	DiscoveryV5Flag = cli.BoolFlag{
		Name:  "v5disc",
		Usage: "Enables the experimental RLPx V5 (Topic Discovery) mechanism",
	}
	NetrestrictFlag = cli.StringFlag{
		Name:  "netrestrict",
		Usage: "Restricts network communication to the given IP networks (CIDR masks)",
	}

	// ATM the url is left to the user and deployment to
	JSpathFlag = cli.StringFlag{
		Name:  "jspath",
		Usage: "JavaScript root path for `loadScript`",
		Value: ".",
	}

	// Gas price oracle settings
	GpoBlocksFlag = cli.IntFlag{
		Name:  "gpoblocks",
		Usage: "Number of recent blocks to check for gas prices",
		Value: eth.DefaultConfig.GPO.Blocks,
	}
	GpoPercentileFlag = cli.IntFlag{
		Name:  "gpopercentile",
		Usage: "Suggested gas price is the given percentile of a set of recent transaction gas prices",
		Value: eth.DefaultConfig.GPO.Percentile,
	}
	WhisperEnabledFlag = cli.BoolFlag{
		Name:  "shh",
		Usage: "Enable Whisper",
	}
	WhisperMaxMessageSizeFlag = cli.IntFlag{
		Name:  "shh.maxmessagesize",
		Usage: "Max message size accepted",
		Value: int(whisper.DefaultMaxMessageSize),
	}
	WhisperMinPOWFlag = cli.Float64Flag{
		Name:  "shh.pow",
		Usage: "Minimum POW accepted",
		Value: whisper.DefaultMinimumPoW,
	}
	WhisperRestrictConnectionBetweenLightClientsFlag = cli.BoolFlag{
		Name:  "shh.restrict-light",
		Usage: "Restrict connection between two whisper light clients",
	}

	// Metrics flags
	MetricsEnabledFlag = cli.BoolFlag{
		Name:  "metrics",
		Usage: "Enable metrics collection and reporting",
	}
	MetricsEnabledExpensiveFlag = cli.BoolFlag{
		Name:  "metrics.expensive",
		Usage: "Enable expensive metrics collection and reporting",
	}
	MetricsEnableInfluxDBFlag = cli.BoolFlag{
		Name:  "metrics.influxdb",
		Usage: "Enable metrics export/push to an external InfluxDB database",
	}
	MetricsInfluxDBEndpointFlag = cli.StringFlag{
		Name:  "metrics.influxdb.endpoint",
		Usage: "InfluxDB API endpoint to report metrics to",
		Value: "http://localhost:8086",
	}
	MetricsInfluxDBDatabaseFlag = cli.StringFlag{
		Name:  "metrics.influxdb.database",
		Usage: "InfluxDB database name to push reported metrics to",
		Value: "geth",
	}
	MetricsInfluxDBUsernameFlag = cli.StringFlag{
		Name:  "metrics.influxdb.username",
		Usage: "Username to authorize access to the database",
		Value: "test",
	}
	MetricsInfluxDBPasswordFlag = cli.StringFlag{
		Name:  "metrics.influxdb.password",
		Usage: "Password to authorize access to the database",
		Value: "test",
	}
	// Tags are part of every measurement sent to InfluxDB. Queries on tags are faster in InfluxDB.
	// For example `host` tag could be used so that we can group all nodes and average a measurement
	// across all of them, but also so that we can select a specific node and inspect its measurements.
	// https://docs.influxdata.com/influxdb/v1.4/concepts/key_concepts/#tag-key
	MetricsInfluxDBTagsFlag = cli.StringFlag{
		Name:  "metrics.influxdb.tags",
		Usage: "Comma-separated InfluxDB tags (key/values) attached to all measurements",
		Value: "host=localhost",
	}

	EWASMInterpreterFlag = cli.StringFlag{
		Name:  "vm.ewasm",
		Usage: "External ewasm configuration (default = built-in interpreter)",
		Value: "",
	}
	EVMInterpreterFlag = cli.StringFlag{
		Name:  "vm.evm",
		Usage: "External EVM configuration (default = built-in interpreter)",
		Value: "",
	}

	// Quorum
	// immutability threshold which can be passed as a parameter at geth start
	QuorumImmutabilityThreshold = cli.IntFlag{
		Name:  "immutabilitythreshold",
		Usage: "overrides the default immutability threshold for Quorum nodes. Its the threshold beyond which block data will be moved to ancient db",
		Value: 3162240,
	}
	// Raft flags
	RaftModeFlag = cli.BoolFlag{
		Name:  "raft",
		Usage: "If enabled, uses Raft instead of Quorum Chain for consensus",
	}
	RaftBlockTimeFlag = cli.IntFlag{
		Name:  "raftblocktime",
		Usage: "Amount of time between raft block creations in milliseconds",
		Value: 50,
	}
	RaftJoinExistingFlag = cli.IntFlag{
		Name:  "raftjoinexisting",
		Usage: "The raft ID to assume when joining an pre-existing cluster",
		Value: 0,
	}
	EmitCheckpointsFlag = cli.BoolFlag{
		Name:  "emitcheckpoints",
		Usage: "If enabled, emit specially formatted logging checkpoints",
	}
	RaftPortFlag = cli.IntFlag{
		Name:  "raftport",
		Usage: "The port to bind for the raft transport",
		Value: 50400,
	}
	RaftDNSEnabledFlag = cli.BoolFlag{
		Name:  "raftdnsenable",
		Usage: "Enable DNS resolution of peers",
	}

	// Permission
	EnableNodePermissionFlag = cli.BoolFlag{
		Name:  "permissioned",
		Usage: "If enabled, the node will allow only a defined list of nodes to connect",
	}
	AllowedFutureBlockTimeFlag = cli.Uint64Flag{
		Name:  "allowedfutureblocktime",
		Usage: "Max time (in seconds) from current time allowed for blocks, before they're considered future blocks",
		Value: 0,
	}
	// Plugins settings
	PluginSettingsFlag = cli.StringFlag{
		Name:  "plugins",
		Usage: "The URI of configuration which describes plugins being used. E.g.: file:///opt/geth/plugins.json",
	}
	PluginLocalVerifyFlag = cli.BoolFlag{
		Name:  "plugins.localverify",
		Usage: "If enabled, verify plugin integrity from local file system. This requires plugin signature file and PGP public key file to be available",
	}
	PluginPublicKeyFlag = cli.StringFlag{
		Name:  "plugins.publickey",
		Usage: fmt.Sprintf("The URI of PGP public key for local plugin verification. E.g.: file:///opt/geth/pubkey.pgp.asc. This flag is only valid if --%s is set (default = file:///<pluginBaseDir>/%s)", PluginLocalVerifyFlag.Name, plugin.DefaultPublicKeyFile),
	}
	PluginSkipVerifyFlag = cli.BoolFlag{
		Name:  "plugins.skipverify",
		Usage: "If enabled, plugin integrity is NOT verified",
	}
	// account plugin flags
	AccountPluginNewAccountConfigFlag = cli.StringFlag{
		Name:  "plugins.account.config",
		Usage: "Value will be passed to an account plugin if being used.  See the account plugin implementation's documentation for further details",
	}
	// Istanbul settings
	IstanbulRequestTimeoutFlag = cli.Uint64Flag{
		Name:  "istanbul.requesttimeout",
		Usage: "Timeout for each Istanbul round in milliseconds",
		Value: eth.DefaultConfig.Istanbul.RequestTimeout,
	}
	IstanbulBlockPeriodFlag = cli.Uint64Flag{
		Name:  "istanbul.blockperiod",
		Usage: "Default minimum difference between two consecutive block's timestamps in seconds",
		Value: eth.DefaultConfig.Istanbul.BlockPeriod,
	}
)

// MakeDataDir retrieves the currently requested data directory, terminating
// if none (or the empty string) is specified. If the node is starting a testnet,
// the a subdirectory of the specified datadir will be used.
func MakeDataDir(ctx *cli.Context) string {
	if path := ctx.GlobalString(DataDirFlag.Name); path != "" {
		if ctx.GlobalBool(TestnetFlag.Name) {
			return filepath.Join(path, "testnet")
		}
		if ctx.GlobalBool(RinkebyFlag.Name) {
			return filepath.Join(path, "rinkeby")
		}
		if ctx.GlobalBool(GoerliFlag.Name) {
			return filepath.Join(path, "goerli")
		}
		return path
	}
	Fatalf("Cannot determine default data directory, please set manually (--datadir)")
	return ""
}

// setNodeKey creates a node key from set command line flags, either loading it
// from a file or as a specified hex value. If neither flags were provided, this
// method returns nil and an emphemeral key is to be generated.
func setNodeKey(ctx *cli.Context, cfg *p2p.Config) {
	var (
		hex  = ctx.GlobalString(NodeKeyHexFlag.Name)
		file = ctx.GlobalString(NodeKeyFileFlag.Name)
		key  *ecdsa.PrivateKey
		err  error
	)
	switch {
	case file != "" && hex != "":
		Fatalf("Options %q and %q are mutually exclusive", NodeKeyFileFlag.Name, NodeKeyHexFlag.Name)
	case file != "":
		if key, err = crypto.LoadECDSA(file); err != nil {
			Fatalf("Option %q: %v", NodeKeyFileFlag.Name, err)
		}
		cfg.PrivateKey = key
	case hex != "":
		if key, err = crypto.HexToECDSA(hex); err != nil {
			Fatalf("Option %q: %v", NodeKeyHexFlag.Name, err)
		}
		cfg.PrivateKey = key
	}
}

// setNodeUserIdent creates the user identifier from CLI flags.
func setNodeUserIdent(ctx *cli.Context, cfg *node.Config) {
	if identity := ctx.GlobalString(IdentityFlag.Name); len(identity) > 0 {
		cfg.UserIdent = identity
	}
}

// setBootstrapNodes creates a list of bootstrap nodes from the command line
// flags, reverting to pre-configured ones if none have been specified.
func setBootstrapNodes(ctx *cli.Context, cfg *p2p.Config) {
	urls := params.MainnetBootnodes
	switch {
	case ctx.GlobalIsSet(BootnodesFlag.Name) || ctx.GlobalIsSet(BootnodesV4Flag.Name):
		if ctx.GlobalIsSet(BootnodesV4Flag.Name) {
			urls = strings.Split(ctx.GlobalString(BootnodesV4Flag.Name), ",")
		} else {
			urls = strings.Split(ctx.GlobalString(BootnodesFlag.Name), ",")
		}
	case ctx.GlobalBool(TestnetFlag.Name):
		urls = params.TestnetBootnodes
	case ctx.GlobalBool(RinkebyFlag.Name):
		urls = params.RinkebyBootnodes
	case ctx.GlobalBool(GoerliFlag.Name):
		urls = params.GoerliBootnodes
	case cfg.BootstrapNodes != nil:
		return // already set, don't apply defaults.
	}

	cfg.BootstrapNodes = make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		if url != "" {
			node, err := enode.Parse(enode.ValidSchemes, url)
			if err != nil {
				log.Crit("Bootstrap URL invalid", "enode", url, "err", err)
				continue
			}
			cfg.BootstrapNodes = append(cfg.BootstrapNodes, node)
		}
	}
}

// setBootstrapNodesV5 creates a list of bootstrap nodes from the command line
// flags, reverting to pre-configured ones if none have been specified.
func setBootstrapNodesV5(ctx *cli.Context, cfg *p2p.Config) {
	urls := params.DiscoveryV5Bootnodes
	switch {
	case ctx.GlobalIsSet(BootnodesFlag.Name) || ctx.GlobalIsSet(BootnodesV5Flag.Name):
		if ctx.GlobalIsSet(BootnodesV5Flag.Name) {
			urls = strings.Split(ctx.GlobalString(BootnodesV5Flag.Name), ",")
		} else {
			urls = strings.Split(ctx.GlobalString(BootnodesFlag.Name), ",")
		}
	case ctx.GlobalBool(RinkebyFlag.Name):
		urls = params.RinkebyBootnodes
	case ctx.GlobalBool(GoerliFlag.Name):
		urls = params.GoerliBootnodes
	case cfg.BootstrapNodesV5 != nil:
		return // already set, don't apply defaults.
	}

	cfg.BootstrapNodesV5 = make([]*discv5.Node, 0, len(urls))
	for _, url := range urls {
		if url != "" {
			node, err := discv5.ParseNode(url)
			if err != nil {
				log.Error("Bootstrap URL invalid", "enode", url, "err", err)
				continue
			}
			cfg.BootstrapNodesV5 = append(cfg.BootstrapNodesV5, node)
		}
	}
}

// setListenAddress creates a TCP listening address string from set command
// line flags.
func setListenAddress(ctx *cli.Context, cfg *p2p.Config) {
	if ctx.GlobalIsSet(ListenPortFlag.Name) {
		cfg.ListenAddr = fmt.Sprintf(":%d", ctx.GlobalInt(ListenPortFlag.Name))
	}
}

// setNAT creates a port mapper from command line flags.
func setNAT(ctx *cli.Context, cfg *p2p.Config) {
	if ctx.GlobalIsSet(NATFlag.Name) {
		natif, err := nat.Parse(ctx.GlobalString(NATFlag.Name))
		if err != nil {
			Fatalf("Option %s: %v", NATFlag.Name, err)
		}
		cfg.NAT = natif
	}
}

// splitAndTrim splits input separated by a comma
// and trims excessive white space from the substrings.
func splitAndTrim(input string) []string {
	result := strings.Split(input, ",")
	for i, r := range result {
		result[i] = strings.TrimSpace(r)
	}
	return result
}

// setHTTP creates the HTTP RPC listener interface string from the set
// command line flags, returning empty if the HTTP endpoint is disabled.
func setHTTP(ctx *cli.Context, cfg *node.Config) {
	if ctx.GlobalBool(RPCEnabledFlag.Name) && cfg.HTTPHost == "" {
		cfg.HTTPHost = "127.0.0.1"
		if ctx.GlobalIsSet(RPCListenAddrFlag.Name) {
			cfg.HTTPHost = ctx.GlobalString(RPCListenAddrFlag.Name)
		}
	}
	if ctx.GlobalIsSet(RPCPortFlag.Name) {
		cfg.HTTPPort = ctx.GlobalInt(RPCPortFlag.Name)
	}
	if ctx.GlobalIsSet(RPCCORSDomainFlag.Name) {
		cfg.HTTPCors = splitAndTrim(ctx.GlobalString(RPCCORSDomainFlag.Name))
	}
	if ctx.GlobalIsSet(RPCApiFlag.Name) {
		cfg.HTTPModules = splitAndTrim(ctx.GlobalString(RPCApiFlag.Name))
	}
	if ctx.GlobalIsSet(RPCVirtualHostsFlag.Name) {
		cfg.HTTPVirtualHosts = splitAndTrim(ctx.GlobalString(RPCVirtualHostsFlag.Name))
	}
}

// setGraphQL creates the GraphQL listener interface string from the set
// command line flags, returning empty if the GraphQL endpoint is disabled.
func setGraphQL(ctx *cli.Context, cfg *node.Config) {
	if ctx.GlobalBool(GraphQLEnabledFlag.Name) && cfg.GraphQLHost == "" {
		cfg.GraphQLHost = "127.0.0.1"
		if ctx.GlobalIsSet(GraphQLListenAddrFlag.Name) {
			cfg.GraphQLHost = ctx.GlobalString(GraphQLListenAddrFlag.Name)
		}
	}
	cfg.GraphQLPort = ctx.GlobalInt(GraphQLPortFlag.Name)
	if ctx.GlobalIsSet(GraphQLCORSDomainFlag.Name) {
		cfg.GraphQLCors = splitAndTrim(ctx.GlobalString(GraphQLCORSDomainFlag.Name))
	}
	if ctx.GlobalIsSet(GraphQLVirtualHostsFlag.Name) {
		cfg.GraphQLVirtualHosts = splitAndTrim(ctx.GlobalString(GraphQLVirtualHostsFlag.Name))
	}
}

// setWS creates the WebSocket RPC listener interface string from the set
// command line flags, returning empty if the HTTP endpoint is disabled.
func setWS(ctx *cli.Context, cfg *node.Config) {
	if ctx.GlobalBool(WSEnabledFlag.Name) && cfg.WSHost == "" {
		cfg.WSHost = "127.0.0.1"
		if ctx.GlobalIsSet(WSListenAddrFlag.Name) {
			cfg.WSHost = ctx.GlobalString(WSListenAddrFlag.Name)
		}
	}
	if ctx.GlobalIsSet(WSPortFlag.Name) {
		cfg.WSPort = ctx.GlobalInt(WSPortFlag.Name)
	}
	if ctx.GlobalIsSet(WSAllowedOriginsFlag.Name) {
		cfg.WSOrigins = splitAndTrim(ctx.GlobalString(WSAllowedOriginsFlag.Name))
	}
	if ctx.GlobalIsSet(WSApiFlag.Name) {
		cfg.WSModules = splitAndTrim(ctx.GlobalString(WSApiFlag.Name))
	}
}

// setIPC creates an IPC path configuration from the set command line flags,
// returning an empty string if IPC was explicitly disabled, or the set path.
func setIPC(ctx *cli.Context, cfg *node.Config) {
	CheckExclusive(ctx, IPCDisabledFlag, IPCPathFlag)
	switch {
	case ctx.GlobalBool(IPCDisabledFlag.Name):
		cfg.IPCPath = ""
	case ctx.GlobalIsSet(IPCPathFlag.Name):
		cfg.IPCPath = ctx.GlobalString(IPCPathFlag.Name)
	}
}

// setLes configures the les server and ultra light client settings from the command line flags.
func setLes(ctx *cli.Context, cfg *eth.Config) {
	if ctx.GlobalIsSet(LightLegacyServFlag.Name) {
		cfg.LightServ = ctx.GlobalInt(LightLegacyServFlag.Name)
	}
	if ctx.GlobalIsSet(LightServeFlag.Name) {
		cfg.LightServ = ctx.GlobalInt(LightServeFlag.Name)
	}
	if ctx.GlobalIsSet(LightIngressFlag.Name) {
		cfg.LightIngress = ctx.GlobalInt(LightIngressFlag.Name)
	}
	if ctx.GlobalIsSet(LightEgressFlag.Name) {
		cfg.LightEgress = ctx.GlobalInt(LightEgressFlag.Name)
	}
	if ctx.GlobalIsSet(LightLegacyPeersFlag.Name) {
		cfg.LightPeers = ctx.GlobalInt(LightLegacyPeersFlag.Name)
	}
	if ctx.GlobalIsSet(LightMaxPeersFlag.Name) {
		cfg.LightPeers = ctx.GlobalInt(LightMaxPeersFlag.Name)
	}
	if ctx.GlobalIsSet(UltraLightServersFlag.Name) {
		cfg.UltraLightServers = strings.Split(ctx.GlobalString(UltraLightServersFlag.Name), ",")
	}
	if ctx.GlobalIsSet(UltraLightFractionFlag.Name) {
		cfg.UltraLightFraction = ctx.GlobalInt(UltraLightFractionFlag.Name)
	}
	if cfg.UltraLightFraction <= 0 && cfg.UltraLightFraction > 100 {
		log.Error("Ultra light fraction is invalid", "had", cfg.UltraLightFraction, "updated", eth.DefaultConfig.UltraLightFraction)
		cfg.UltraLightFraction = eth.DefaultConfig.UltraLightFraction
	}
	if ctx.GlobalIsSet(UltraLightOnlyAnnounceFlag.Name) {
		cfg.UltraLightOnlyAnnounce = ctx.GlobalBool(UltraLightOnlyAnnounceFlag.Name)
	}
}

// makeDatabaseHandles raises out the number of allowed file handles per process
// for Geth and returns half of the allowance to assign to the database.
func makeDatabaseHandles() int {
	limit, err := fdlimit.Maximum()
	if err != nil {
		Fatalf("Failed to retrieve file descriptor allowance: %v", err)
	}
	raised, err := fdlimit.Raise(uint64(limit))
	if err != nil {
		Fatalf("Failed to raise file descriptor allowance: %v", err)
	}
	return int(raised / 2) // Leave half for networking and other stuff
}

// MakeAddress converts an account specified directly as a hex encoded string or
// a key index in the key store to an internal account representation.
func MakeAddress(ks *keystore.KeyStore, account string) (accounts.Account, error) {
	// If the specified account is a valid address, return it
	if common.IsHexAddress(account) {
		return accounts.Account{Address: common.HexToAddress(account)}, nil
	}
	// Otherwise try to interpret the account as a keystore index
	index, err := strconv.Atoi(account)
	if err != nil || index < 0 {
		return accounts.Account{}, fmt.Errorf("invalid account address or index %q", account)
	}
	log.Warn("-------------------------------------------------------------------")
	log.Warn("Referring to accounts by order in the keystore folder is dangerous!")
	log.Warn("This functionality is deprecated and will be removed in the future!")
	log.Warn("Please use explicit addresses! (can search via `geth account list`)")
	log.Warn("-------------------------------------------------------------------")

	accs := ks.Accounts()
	if len(accs) <= index {
		return accounts.Account{}, fmt.Errorf("index %d higher than number of accounts %d", index, len(accs))
	}
	return accs[index], nil
}

// setEtherbase retrieves the etherbase either from the directly specified
// command line flags or from the keystore if CLI indexed.
func setEtherbase(ctx *cli.Context, ks *keystore.KeyStore, cfg *eth.Config) {
	// Extract the current etherbase, new flag overriding legacy one
	var etherbase string
	if ctx.GlobalIsSet(MinerLegacyEtherbaseFlag.Name) {
		etherbase = ctx.GlobalString(MinerLegacyEtherbaseFlag.Name)
	}
	if ctx.GlobalIsSet(MinerEtherbaseFlag.Name) {
		etherbase = ctx.GlobalString(MinerEtherbaseFlag.Name)
	}
	// Convert the etherbase into an address and configure it
	if etherbase != "" {
		if ks != nil {
			account, err := MakeAddress(ks, etherbase)
			if err != nil {
				Fatalf("Invalid miner etherbase: %v", err)
			}
			cfg.Miner.Etherbase = account.Address
		} else {
			Fatalf("No etherbase configured")
		}
	}
}

// MakePasswordList reads password lines from the file specified by the global --password flag.
func MakePasswordList(ctx *cli.Context) []string {
	path := ctx.GlobalString(PasswordFileFlag.Name)
	if path == "" {
		return nil
	}
	text, err := ioutil.ReadFile(path)
	if err != nil {
		Fatalf("Failed to read password file: %v", err)
	}
	lines := strings.Split(string(text), "\n")
	// Sanitise DOS line endings.
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	return lines
}

func SetP2PConfig(ctx *cli.Context, cfg *p2p.Config) {
	setNodeKey(ctx, cfg)
	setNAT(ctx, cfg)
	setListenAddress(ctx, cfg)
	setBootstrapNodes(ctx, cfg)
	setBootstrapNodesV5(ctx, cfg)

	lightClient := ctx.GlobalString(SyncModeFlag.Name) == "light"
	lightServer := (ctx.GlobalInt(LightLegacyServFlag.Name) != 0 || ctx.GlobalInt(LightServeFlag.Name) != 0)

	lightPeers := ctx.GlobalInt(LightLegacyPeersFlag.Name)
	if ctx.GlobalIsSet(LightMaxPeersFlag.Name) {
		lightPeers = ctx.GlobalInt(LightMaxPeersFlag.Name)
	}
	if lightClient && !ctx.GlobalIsSet(LightLegacyPeersFlag.Name) && !ctx.GlobalIsSet(LightMaxPeersFlag.Name) {
		// dynamic default - for clients we use 1/10th of the default for servers
		lightPeers /= 10
	}

	if ctx.GlobalIsSet(MaxPeersFlag.Name) {
		cfg.MaxPeers = ctx.GlobalInt(MaxPeersFlag.Name)
		if lightServer && !ctx.GlobalIsSet(LightLegacyPeersFlag.Name) && !ctx.GlobalIsSet(LightMaxPeersFlag.Name) {
			cfg.MaxPeers += lightPeers
		}
	} else {
		if lightServer {
			cfg.MaxPeers += lightPeers
		}
		if lightClient && (ctx.GlobalIsSet(LightLegacyPeersFlag.Name) || ctx.GlobalIsSet(LightMaxPeersFlag.Name)) && cfg.MaxPeers < lightPeers {
			cfg.MaxPeers = lightPeers
		}
	}
	if !(lightClient || lightServer) {
		lightPeers = 0
	}
	ethPeers := cfg.MaxPeers - lightPeers
	if lightClient {
		ethPeers = 0
	}
	log.Info("Maximum peer count", "ETH", ethPeers, "LES", lightPeers, "total", cfg.MaxPeers)

	if ctx.GlobalIsSet(MaxPendingPeersFlag.Name) {
		cfg.MaxPendingPeers = ctx.GlobalInt(MaxPendingPeersFlag.Name)
	}
	if ctx.GlobalIsSet(NoDiscoverFlag.Name) || lightClient {
		cfg.NoDiscovery = true
	}

	// if we're running a light client or server, force enable the v5 peer discovery
	// unless it is explicitly disabled with --nodiscover note that explicitly specifying
	// --v5disc overrides --nodiscover, in which case the later only disables v4 discovery
	forceV5Discovery := (lightClient || lightServer) && !ctx.GlobalBool(NoDiscoverFlag.Name)
	if ctx.GlobalIsSet(DiscoveryV5Flag.Name) {
		cfg.DiscoveryV5 = ctx.GlobalBool(DiscoveryV5Flag.Name)
	} else if forceV5Discovery {
		cfg.DiscoveryV5 = true
	}

	if netrestrict := ctx.GlobalString(NetrestrictFlag.Name); netrestrict != "" {
		list, err := netutil.ParseNetlist(netrestrict)
		if err != nil {
			Fatalf("Option %q: %v", NetrestrictFlag.Name, err)
		}
		cfg.NetRestrict = list
	}

	if ctx.GlobalBool(DeveloperFlag.Name) {
		// --dev mode can't use p2p networking.
		cfg.MaxPeers = 0
		cfg.ListenAddr = ":0"
		cfg.NoDiscovery = true
		cfg.DiscoveryV5 = false
	}
}

// SetNodeConfig applies node-related command line flags to the config.
func SetNodeConfig(ctx *cli.Context, cfg *node.Config) {
	SetP2PConfig(ctx, &cfg.P2P)
	setIPC(ctx, cfg)
	setHTTP(ctx, cfg)
	setGraphQL(ctx, cfg)
	setWS(ctx, cfg)
	setNodeUserIdent(ctx, cfg)
	setDataDir(ctx, cfg)
	setSmartCard(ctx, cfg)

	if ctx.GlobalIsSet(ExternalSignerFlag.Name) {
		cfg.ExternalSigner = ctx.GlobalString(ExternalSignerFlag.Name)
	}

	if ctx.GlobalIsSet(KeyStoreDirFlag.Name) {
		cfg.KeyStoreDir = ctx.GlobalString(KeyStoreDirFlag.Name)
	}
	if ctx.GlobalIsSet(LightKDFFlag.Name) {
		cfg.UseLightweightKDF = ctx.GlobalBool(LightKDFFlag.Name)
	}
	if ctx.GlobalIsSet(NoUSBFlag.Name) {
		cfg.NoUSB = ctx.GlobalBool(NoUSBFlag.Name)
	}
	if ctx.GlobalIsSet(InsecureUnlockAllowedFlag.Name) {
		cfg.InsecureUnlockAllowed = ctx.GlobalBool(InsecureUnlockAllowedFlag.Name)
	}

	// Quorum
	if ctx.GlobalIsSet(EnableNodePermissionFlag.Name) {
		cfg.EnableNodePermission = ctx.GlobalBool(EnableNodePermissionFlag.Name)
	}

}

func setSmartCard(ctx *cli.Context, cfg *node.Config) {
	// Skip enabling smartcards if no path is set
	path := ctx.GlobalString(SmartCardDaemonPathFlag.Name)
	if path == "" {
		return
	}
	// Sanity check that the smartcard path is valid
	fi, err := os.Stat(path)
	if err != nil {
		log.Info("Smartcard socket not found, disabling", "err", err)
		return
	}
	if fi.Mode()&os.ModeType != os.ModeSocket {
		log.Error("Invalid smartcard daemon path", "path", path, "type", fi.Mode().String())
		return
	}
	// Smartcard daemon path exists and is a socket, enable it
	cfg.SmartCardDaemonPath = path
}

func setDataDir(ctx *cli.Context, cfg *node.Config) {
	switch {
	case ctx.GlobalIsSet(DataDirFlag.Name):
		cfg.DataDir = ctx.GlobalString(DataDirFlag.Name)
	case ctx.GlobalBool(DeveloperFlag.Name):
		cfg.DataDir = "" // unless explicitly requested, use memory databases
	case ctx.GlobalBool(TestnetFlag.Name) && cfg.DataDir == node.DefaultDataDir():
		cfg.DataDir = filepath.Join(node.DefaultDataDir(), "testnet")
	case ctx.GlobalBool(RinkebyFlag.Name) && cfg.DataDir == node.DefaultDataDir():
		cfg.DataDir = filepath.Join(node.DefaultDataDir(), "rinkeby")
	case ctx.GlobalBool(GoerliFlag.Name) && cfg.DataDir == node.DefaultDataDir():
		cfg.DataDir = filepath.Join(node.DefaultDataDir(), "goerli")
	}
	if err := SetPlugins(ctx, cfg); err != nil {
		Fatalf(err.Error())
	}
}

// Quorum
//
// Read plugin settings from --plugins flag. Overwrite settings defined in --config if any
func SetPlugins(ctx *cli.Context, cfg *node.Config) error {
	if ctx.GlobalIsSet(PluginSettingsFlag.Name) {
		// validate flag combination
		if ctx.GlobalBool(PluginSkipVerifyFlag.Name) && ctx.GlobalBool(PluginLocalVerifyFlag.Name) {
			return fmt.Errorf("only --%s or --%s must be set", PluginSkipVerifyFlag.Name, PluginLocalVerifyFlag.Name)
		}
		if !ctx.GlobalBool(PluginLocalVerifyFlag.Name) && ctx.GlobalIsSet(PluginPublicKeyFlag.Name) {
			return fmt.Errorf("--%s is required for setting --%s", PluginLocalVerifyFlag.Name, PluginPublicKeyFlag.Name)
		}
		pluginSettingsURL, err := url.Parse(ctx.GlobalString(PluginSettingsFlag.Name))
		if err != nil {
			return fmt.Errorf("plugins: Invalid URL for --%s due to %s", PluginSettingsFlag.Name, err)
		}
		var pluginSettings plugin.Settings
		r, err := urlReader(pluginSettingsURL)
		if err != nil {
			return fmt.Errorf("plugins: unable to create reader due to %s", err)
		}
		defer func() {
			_ = r.Close()
		}()
		if err := json.NewDecoder(r).Decode(&pluginSettings); err != nil {
			return fmt.Errorf("plugins: unable to parse settings due to %s", err)
		}
		pluginSettings.SetDefaults()
		cfg.Plugins = &pluginSettings
	}
	return nil
}

func urlReader(u *url.URL) (io.ReadCloser, error) {
	s := u.Scheme
	switch s {
	case "file":
		return os.Open(filepath.Join(u.Host, u.Path))
	}
	return nil, fmt.Errorf("unsupported scheme %s", s)
}

func setGPO(ctx *cli.Context, cfg *gasprice.Config) {
	if ctx.GlobalIsSet(GpoBlocksFlag.Name) {
		cfg.Blocks = ctx.GlobalInt(GpoBlocksFlag.Name)
	}
	if ctx.GlobalIsSet(GpoPercentileFlag.Name) {
		cfg.Percentile = ctx.GlobalInt(GpoPercentileFlag.Name)
	}
}

func setTxPool(ctx *cli.Context, cfg *core.TxPoolConfig) {
	if ctx.GlobalIsSet(TxPoolLocalsFlag.Name) {
		locals := strings.Split(ctx.GlobalString(TxPoolLocalsFlag.Name), ",")
		for _, account := range locals {
			if trimmed := strings.TrimSpace(account); !common.IsHexAddress(trimmed) {
				Fatalf("Invalid account in --txpool.locals: %s", trimmed)
			} else {
				cfg.Locals = append(cfg.Locals, common.HexToAddress(account))
			}
		}
	}
	if ctx.GlobalIsSet(TxPoolNoLocalsFlag.Name) {
		cfg.NoLocals = ctx.GlobalBool(TxPoolNoLocalsFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolJournalFlag.Name) {
		cfg.Journal = ctx.GlobalString(TxPoolJournalFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolRejournalFlag.Name) {
		cfg.Rejournal = ctx.GlobalDuration(TxPoolRejournalFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolPriceLimitFlag.Name) {
		cfg.PriceLimit = ctx.GlobalUint64(TxPoolPriceLimitFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolPriceBumpFlag.Name) {
		cfg.PriceBump = ctx.GlobalUint64(TxPoolPriceBumpFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolAccountSlotsFlag.Name) {
		cfg.AccountSlots = ctx.GlobalUint64(TxPoolAccountSlotsFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolGlobalSlotsFlag.Name) {
		cfg.GlobalSlots = ctx.GlobalUint64(TxPoolGlobalSlotsFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolAccountQueueFlag.Name) {
		cfg.AccountQueue = ctx.GlobalUint64(TxPoolAccountQueueFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolGlobalQueueFlag.Name) {
		cfg.GlobalQueue = ctx.GlobalUint64(TxPoolGlobalQueueFlag.Name)
	}
	if ctx.GlobalIsSet(TxPoolLifetimeFlag.Name) {
		cfg.Lifetime = ctx.GlobalDuration(TxPoolLifetimeFlag.Name)
	}
}

func setEthash(ctx *cli.Context, cfg *eth.Config) {
	if ctx.GlobalIsSet(EthashCacheDirFlag.Name) {
		cfg.Ethash.CacheDir = ctx.GlobalString(EthashCacheDirFlag.Name)
	}
	if ctx.GlobalIsSet(EthashDatasetDirFlag.Name) {
		cfg.Ethash.DatasetDir = ctx.GlobalString(EthashDatasetDirFlag.Name)
	}
	if ctx.GlobalIsSet(EthashCachesInMemoryFlag.Name) {
		cfg.Ethash.CachesInMem = ctx.GlobalInt(EthashCachesInMemoryFlag.Name)
	}
	if ctx.GlobalIsSet(EthashCachesOnDiskFlag.Name) {
		cfg.Ethash.CachesOnDisk = ctx.GlobalInt(EthashCachesOnDiskFlag.Name)
	}
	if ctx.GlobalIsSet(EthashDatasetsInMemoryFlag.Name) {
		cfg.Ethash.DatasetsInMem = ctx.GlobalInt(EthashDatasetsInMemoryFlag.Name)
	}
	if ctx.GlobalIsSet(EthashDatasetsOnDiskFlag.Name) {
		cfg.Ethash.DatasetsOnDisk = ctx.GlobalInt(EthashDatasetsOnDiskFlag.Name)
	}
}

func setMiner(ctx *cli.Context, cfg *miner.Config) {
	if ctx.GlobalIsSet(MinerNotifyFlag.Name) {
		cfg.Notify = strings.Split(ctx.GlobalString(MinerNotifyFlag.Name), ",")
	}
	if ctx.GlobalIsSet(MinerLegacyExtraDataFlag.Name) {
		cfg.ExtraData = []byte(ctx.GlobalString(MinerLegacyExtraDataFlag.Name))
	}
	if ctx.GlobalIsSet(MinerExtraDataFlag.Name) {
		cfg.ExtraData = []byte(ctx.GlobalString(MinerExtraDataFlag.Name))
	}
	if ctx.GlobalIsSet(MinerLegacyGasTargetFlag.Name) {
		cfg.GasFloor = ctx.GlobalUint64(MinerLegacyGasTargetFlag.Name)
	}
	if ctx.GlobalIsSet(MinerGasTargetFlag.Name) {
		cfg.GasFloor = ctx.GlobalUint64(MinerGasTargetFlag.Name)
	}
	if ctx.GlobalIsSet(MinerGasLimitFlag.Name) {
		cfg.GasCeil = ctx.GlobalUint64(MinerGasLimitFlag.Name)
	}
	if ctx.GlobalIsSet(MinerLegacyGasPriceFlag.Name) {
		cfg.GasPrice = GlobalBig(ctx, MinerLegacyGasPriceFlag.Name)
	}
	if ctx.GlobalIsSet(MinerGasPriceFlag.Name) {
		cfg.GasPrice = GlobalBig(ctx, MinerGasPriceFlag.Name)
	}
	if ctx.GlobalIsSet(MinerRecommitIntervalFlag.Name) {
		cfg.Recommit = ctx.Duration(MinerRecommitIntervalFlag.Name)
	}
	if ctx.GlobalIsSet(MinerNoVerfiyFlag.Name) {
		cfg.Noverify = ctx.Bool(MinerNoVerfiyFlag.Name)
	}
	if ctx.GlobalIsSet(AllowedFutureBlockTimeFlag.Name) {
		cfg.AllowedFutureBlockTime = ctx.GlobalUint64(AllowedFutureBlockTimeFlag.Name) //Quorum
	}
}

func setWhitelist(ctx *cli.Context, cfg *eth.Config) {
	whitelist := ctx.GlobalString(WhitelistFlag.Name)
	if whitelist == "" {
		return
	}
	cfg.Whitelist = make(map[uint64]common.Hash)
	for _, entry := range strings.Split(whitelist, ",") {
		parts := strings.Split(entry, "=")
		if len(parts) != 2 {
			Fatalf("Invalid whitelist entry: %s", entry)
		}
		number, err := strconv.ParseUint(parts[0], 0, 64)
		if err != nil {
			Fatalf("Invalid whitelist block number %s: %v", parts[0], err)
		}
		var hash common.Hash
		if err = hash.UnmarshalText([]byte(parts[1])); err != nil {
			Fatalf("Invalid whitelist hash %s: %v", parts[1], err)
		}
		cfg.Whitelist[number] = hash
	}
}

// Quorum
func setIstanbul(ctx *cli.Context, cfg *eth.Config) {
	if ctx.GlobalIsSet(IstanbulRequestTimeoutFlag.Name) {
		cfg.Istanbul.RequestTimeout = ctx.GlobalUint64(IstanbulRequestTimeoutFlag.Name)
	}
	if ctx.GlobalIsSet(IstanbulBlockPeriodFlag.Name) {
		cfg.Istanbul.BlockPeriod = ctx.GlobalUint64(IstanbulBlockPeriodFlag.Name)
	}
}

func setRaft(ctx *cli.Context, cfg *eth.Config) {
	cfg.RaftMode = ctx.GlobalBool(RaftModeFlag.Name)
}

// CheckExclusive verifies that only a single instance of the provided flags was
// set by the user. Each flag might optionally be followed by a string type to
// specialize it further.
func CheckExclusive(ctx *cli.Context, args ...interface{}) {
	set := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		// Make sure the next argument is a flag and skip if not set
		flag, ok := args[i].(cli.Flag)
		if !ok {
			panic(fmt.Sprintf("invalid argument, not cli.Flag type: %T", args[i]))
		}
		// Check if next arg extends current and expand its name if so
		name := flag.GetName()

		if i+1 < len(args) {
			switch option := args[i+1].(type) {
			case string:
				// Extended flag check, make sure value set doesn't conflict with passed in option
				if ctx.GlobalString(flag.GetName()) == option {
					name += "=" + option
					set = append(set, "--"+name)
				}
				// shift arguments and continue
				i++
				continue

			case cli.Flag:
			default:
				panic(fmt.Sprintf("invalid argument, not cli.Flag or string extension: %T", args[i+1]))
			}
		}
		// Mark the flag if it's set
		if ctx.GlobalIsSet(flag.GetName()) {
			set = append(set, "--"+name)
		}
	}
	if len(set) > 1 {
		Fatalf("Flags %v can't be used at the same time", strings.Join(set, ", "))
	}
}

// SetShhConfig applies shh-related command line flags to the config.
func SetShhConfig(ctx *cli.Context, stack *node.Node, cfg *whisper.Config) {
	if ctx.GlobalIsSet(WhisperMaxMessageSizeFlag.Name) {
		cfg.MaxMessageSize = uint32(ctx.GlobalUint(WhisperMaxMessageSizeFlag.Name))
	}
	if ctx.GlobalIsSet(WhisperMinPOWFlag.Name) {
		cfg.MinimumAcceptedPOW = ctx.GlobalFloat64(WhisperMinPOWFlag.Name)
	}
	if ctx.GlobalIsSet(WhisperRestrictConnectionBetweenLightClientsFlag.Name) {
		cfg.RestrictConnectionBetweenLightClients = true
	}
}

// SetEthConfig applies eth-related command line flags to the config.
func SetEthConfig(ctx *cli.Context, stack *node.Node, cfg *eth.Config) {
	// Avoid conflicting network flags
	CheckExclusive(ctx, DeveloperFlag, TestnetFlag, RinkebyFlag, GoerliFlag)
	CheckExclusive(ctx, LightLegacyServFlag, LightServeFlag, SyncModeFlag, "light")
	CheckExclusive(ctx, DeveloperFlag, ExternalSignerFlag) // Can't use both ephemeral unlocked and external signer

	var ks *keystore.KeyStore
	if keystores := stack.AccountManager().Backends(keystore.KeyStoreType); len(keystores) > 0 {
		ks = keystores[0].(*keystore.KeyStore)
	}
	setEtherbase(ctx, ks, cfg)
	setGPO(ctx, &cfg.GPO)
	setTxPool(ctx, &cfg.TxPool)
	setEthash(ctx, cfg)
	setMiner(ctx, &cfg.Miner)
	setWhitelist(ctx, cfg)
	setLes(ctx, cfg)

	// Quorum
	setIstanbul(ctx, cfg)
	setRaft(ctx, cfg)

	if ctx.GlobalIsSet(SyncModeFlag.Name) {
		cfg.SyncMode = *GlobalTextMarshaler(ctx, SyncModeFlag.Name).(*downloader.SyncMode)
	}
	if ctx.GlobalIsSet(NetworkIdFlag.Name) {
		cfg.NetworkId = ctx.GlobalUint64(NetworkIdFlag.Name)
	}
	if ctx.GlobalIsSet(CacheFlag.Name) || ctx.GlobalIsSet(CacheDatabaseFlag.Name) {
		cfg.DatabaseCache = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheDatabaseFlag.Name) / 100
	}
	cfg.DatabaseHandles = makeDatabaseHandles()
	if ctx.GlobalIsSet(AncientFlag.Name) {
		cfg.DatabaseFreezer = ctx.GlobalString(AncientFlag.Name)
	}

	if gcmode := ctx.GlobalString(GCModeFlag.Name); gcmode != "full" && gcmode != "archive" {
		Fatalf("--%s must be either 'full' or 'archive'", GCModeFlag.Name)
	}
	if ctx.GlobalIsSet(GCModeFlag.Name) {
		cfg.NoPruning = ctx.GlobalString(GCModeFlag.Name) == "archive"
	}
	if ctx.GlobalIsSet(CacheNoPrefetchFlag.Name) {
		cfg.NoPrefetch = ctx.GlobalBool(CacheNoPrefetchFlag.Name)
	}
	if ctx.GlobalIsSet(CacheFlag.Name) || ctx.GlobalIsSet(CacheTrieFlag.Name) {
		cfg.TrieCleanCache = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheTrieFlag.Name) / 100
	}
	if ctx.GlobalIsSet(CacheFlag.Name) || ctx.GlobalIsSet(CacheGCFlag.Name) {
		cfg.TrieDirtyCache = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheGCFlag.Name) / 100
	}
	if ctx.GlobalIsSet(DocRootFlag.Name) {
		cfg.DocRoot = ctx.GlobalString(DocRootFlag.Name)
	}
	if ctx.GlobalIsSet(VMEnableDebugFlag.Name) {
		// TODO(fjl): force-enable this in --dev mode
		cfg.EnablePreimageRecording = ctx.GlobalBool(VMEnableDebugFlag.Name)
	}

	if ctx.GlobalIsSet(EWASMInterpreterFlag.Name) {
		cfg.EWASMInterpreter = ctx.GlobalString(EWASMInterpreterFlag.Name)
	}

	if ctx.GlobalIsSet(EVMInterpreterFlag.Name) {
		cfg.EVMInterpreter = ctx.GlobalString(EVMInterpreterFlag.Name)
	}
	if ctx.GlobalIsSet(RPCGlobalGasCap.Name) {
		cfg.RPCGasCap = new(big.Int).SetUint64(ctx.GlobalUint64(RPCGlobalGasCap.Name))
	}

	// set immutability threshold in config
	params.SetQuorumImmutabilityThreshold(ctx.GlobalInt(QuorumImmutabilityThreshold.Name))

	// Override any default configs for hard coded networks.
	switch {
	case ctx.GlobalBool(TestnetFlag.Name):
		if !ctx.GlobalIsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 3
		}
		cfg.Genesis = core.DefaultTestnetGenesisBlock()
	case ctx.GlobalBool(RinkebyFlag.Name):
		if !ctx.GlobalIsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 4
		}
		cfg.Genesis = core.DefaultRinkebyGenesisBlock()
	case ctx.GlobalBool(GoerliFlag.Name):
		if !ctx.GlobalIsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 5
		}
		cfg.Genesis = core.DefaultGoerliGenesisBlock()
	case ctx.GlobalBool(DeveloperFlag.Name):
		if !ctx.GlobalIsSet(NetworkIdFlag.Name) {
			cfg.NetworkId = 1337
		}
		// Create new developer account or reuse existing one
		var (
			developer accounts.Account
			err       error
		)
		if accs := ks.Accounts(); len(accs) > 0 {
			developer = ks.Accounts()[0]
		} else {
			developer, err = ks.NewAccount("")
			if err != nil {
				Fatalf("Failed to create developer account: %v", err)
			}
		}
		if err := ks.Unlock(developer, ""); err != nil {
			Fatalf("Failed to unlock developer account: %v", err)
		}
		log.Info("Using developer account", "address", developer.Address)

		cfg.Genesis = core.DeveloperGenesisBlock(uint64(ctx.GlobalInt(DeveloperPeriodFlag.Name)), developer.Address)
		if !ctx.GlobalIsSet(MinerGasPriceFlag.Name) && !ctx.GlobalIsSet(MinerLegacyGasPriceFlag.Name) {
			cfg.Miner.GasPrice = big.NewInt(1)
		}
	}
}

// SetDashboardConfig applies dashboard related command line flags to the config.
func SetDashboardConfig(ctx *cli.Context, cfg *dashboard.Config) {
	cfg.Host = ctx.GlobalString(DashboardAddrFlag.Name)
	cfg.Port = ctx.GlobalInt(DashboardPortFlag.Name)
	cfg.Refresh = ctx.GlobalDuration(DashboardRefreshFlag.Name)
}

// RegisterEthService adds an Ethereum client to the stack.
func RegisterEthService(stack *node.Node, cfg *eth.Config) chan *eth.Ethereum {
	// Quorum: raft service listens to this channel to get Ethereum backend
	nodeChan := make(chan *eth.Ethereum, 1)
	var err error
	if cfg.SyncMode == downloader.LightSync {
		err = stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
			return les.New(ctx, cfg)
		})
	} else {
		err = stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
			fullNode, err := eth.New(ctx, cfg)
			if fullNode != nil && cfg.LightServ > 0 {
				ls, _ := les.NewLesServer(fullNode, cfg)
				fullNode.AddLesServer(ls)
			}
			nodeChan <- fullNode
			return fullNode, err
		})
	}
	if err != nil {
		Fatalf("Failed to register the Ethereum service: %v", err)
	}

	return nodeChan
}

// RegisterDashboardService adds a dashboard to the stack.
func RegisterDashboardService(stack *node.Node, cfg *dashboard.Config, commit string) {
	stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		return dashboard.New(cfg, commit, ctx.ResolvePath("logs")), nil
	})
}

// RegisterShhService configures Whisper and adds it to the given node.
func RegisterShhService(stack *node.Node, cfg *whisper.Config) {
	if err := stack.Register(func(n *node.ServiceContext) (node.Service, error) {
		return whisper.New(cfg), nil
	}); err != nil {
		Fatalf("Failed to register the Whisper service: %v", err)
	}
}

// RegisterEthStatsService configures the Ethereum Stats daemon and adds it to
// the given node.
func RegisterEthStatsService(stack *node.Node, url string) {
	if err := stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		// Retrieve both eth and les services
		var ethServ *eth.Ethereum
		ctx.Service(&ethServ)

		var lesServ *les.LightEthereum
		ctx.Service(&lesServ)

		// Let ethstats use whichever is not nil
		return ethstats.New(url, ethServ, lesServ)
	}); err != nil {
		Fatalf("Failed to register the Ethereum Stats service: %v", err)
	}
}

// RegisterGraphQLService is a utility function to construct a new service and register it against a node.
func RegisterGraphQLService(stack *node.Node, endpoint string, cors, vhosts []string, timeouts rpc.HTTPTimeouts) {
	if err := stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		// Try to construct the GraphQL service backed by a full node
		var ethServ *eth.Ethereum
		if err := ctx.Service(&ethServ); err == nil {
			return graphql.New(ethServ.APIBackend, endpoint, cors, vhosts, timeouts)
		}
		// Try to construct the GraphQL service backed by a light node
		var lesServ *les.LightEthereum
		if err := ctx.Service(&lesServ); err == nil {
			return graphql.New(lesServ.ApiBackend, endpoint, cors, vhosts, timeouts)
		}
		// Well, this should not have happened, bail out
		return nil, errors.New("no Ethereum service")
	}); err != nil {
		Fatalf("Failed to register the GraphQL service: %v", err)
	}
}

// Quorum
//
// Register plugin manager as a service in geth
func RegisterPluginService(stack *node.Node, cfg *node.Config, skipVerify bool, localVerify bool, publicKey string) {
	if err := cfg.ResolvePluginBaseDir(); err != nil {
		Fatalf("plugins: unable to resolve plugin base dir due to %s", err)
	}
	if err := stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		return plugin.NewPluginManager(cfg.UserIdent, cfg.Plugins, skipVerify, localVerify, publicKey)
	}); err != nil {
		Fatalf("plugins: Failed to register the Plugins service: %v", err)
	}
}

// Configure smart-contract-based permissioning service
func RegisterPermissionService(stack *node.Node) {
	if err := stack.Register(func(sctx *node.ServiceContext) (node.Service, error) {
		permissionConfig, err := permission.ParsePermissionConfig(stack.DataDir())
		if err != nil {
			return nil, fmt.Errorf("loading of %s failed due to %v", params.PERMISSION_MODEL_CONFIG, err)
		}
		// start the permissions management service
		pc, err := permission.NewQuorumPermissionCtrl(stack, &permissionConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load the permission contracts as given in %s due to %v", params.PERMISSION_MODEL_CONFIG, err)
		}
		return pc, nil
	}); err != nil {
		Fatalf("Failed to register the permission service: %v", err)
	}
	log.Info("permission service registered")
}

func RegisterRaftService(stack *node.Node, ctx *cli.Context, nodeCfg *node.Config, ethChan chan *eth.Ethereum) {
	blockTimeMillis := ctx.GlobalInt(RaftBlockTimeFlag.Name)
	datadir := ctx.GlobalString(DataDirFlag.Name)
	joinExistingId := ctx.GlobalInt(RaftJoinExistingFlag.Name)
	useDns := ctx.GlobalBool(RaftDNSEnabledFlag.Name)
	raftPort := uint16(ctx.GlobalInt(RaftPortFlag.Name))

	var myId uint64
	var joinExisting bool

	if err := stack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
		privkey := nodeCfg.NodeKey()
		strId := enode.PubkeyToIDV4(&privkey.PublicKey).String()
		blockTimeNanos := time.Duration(blockTimeMillis) * time.Millisecond
		peers := nodeCfg.StaticNodes()

		if joinExistingId > 0 {
			myId = uint64(joinExistingId)
			joinExisting = true
		} else if len(peers) == 0 {
			Fatalf("Raft-based consensus requires either an initial peers list including all the nodes in the network (in static-nodes.json) including this enode hash (%v)", strId)
		}

		ethereum := <-ethChan
		ethChan <- ethereum
		// myId joinExisting
		return raft.New(ctx, ethereum.BlockChain().Config(), myId, joinExisting, raftPort, blockTimeNanos, ethereum, peers, datadir, useDns)
	}); err != nil {
		Fatalf("Failed to register the Raft service: %v", err)
	}
}

func RegisterExtensionService(stack *node.Node, ethChan chan *eth.Ethereum) {
	registerFunc := func(ctx *node.ServiceContext) (node.Service, error) {
		factory, err := extension.NewServicesFactory(stack, private.P, <-ethChan)
		if err != nil {
			return nil, err
		}
		return factory.BackendService(), nil
	}
	if err := stack.Register(registerFunc); err != nil {
		Fatalf("Failed to register the Extension service: %v", err)
	}
}

func SetupMetrics(ctx *cli.Context) {
	if metrics.Enabled {
		log.Info("Enabling metrics collection")
		var (
			enableExport = ctx.GlobalBool(MetricsEnableInfluxDBFlag.Name)
			endpoint     = ctx.GlobalString(MetricsInfluxDBEndpointFlag.Name)
			database     = ctx.GlobalString(MetricsInfluxDBDatabaseFlag.Name)
			username     = ctx.GlobalString(MetricsInfluxDBUsernameFlag.Name)
			password     = ctx.GlobalString(MetricsInfluxDBPasswordFlag.Name)
		)

		if enableExport {
			tagsMap := SplitTagsFlag(ctx.GlobalString(MetricsInfluxDBTagsFlag.Name))

			log.Info("Enabling metrics export to InfluxDB")

			go influxdb.InfluxDBWithTags(metrics.DefaultRegistry, 10*time.Second, endpoint, database, username, password, "geth.", tagsMap)
		}
	}
}

func SplitTagsFlag(tagsFlag string) map[string]string {
	tags := strings.Split(tagsFlag, ",")
	tagsMap := map[string]string{}

	for _, t := range tags {
		if t != "" {
			kv := strings.Split(t, "=")

			if len(kv) == 2 {
				tagsMap[kv[0]] = kv[1]
			}
		}
	}

	return tagsMap
}

// MakeChainDatabase open an LevelDB using the flags passed to the client and will hard crash if it fails.
func MakeChainDatabase(ctx *cli.Context, stack *node.Node) ethdb.Database {
	var (
		cache   = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheDatabaseFlag.Name) / 100
		handles = makeDatabaseHandles()
	)
	name := "chaindata"
	if ctx.GlobalString(SyncModeFlag.Name) == "light" {
		name = "lightchaindata"
	}
	chainDb, err := stack.OpenDatabaseWithFreezer(name, cache, handles, ctx.GlobalString(AncientFlag.Name), "")
	if err != nil {
		Fatalf("Could not open database: %v", err)
	}
	return chainDb
}

func MakeGenesis(ctx *cli.Context) *core.Genesis {
	var genesis *core.Genesis
	switch {
	case ctx.GlobalBool(TestnetFlag.Name):
		genesis = core.DefaultTestnetGenesisBlock()
	case ctx.GlobalBool(RinkebyFlag.Name):
		genesis = core.DefaultRinkebyGenesisBlock()
	case ctx.GlobalBool(GoerliFlag.Name):
		genesis = core.DefaultGoerliGenesisBlock()
	case ctx.GlobalBool(DeveloperFlag.Name):
		Fatalf("Developer chains are ephemeral")
	}
	return genesis
}

// MakeChain creates a chain manager from set command line flags.
func MakeChain(ctx *cli.Context, stack *node.Node, useExist bool) (chain *core.BlockChain, chainDb ethdb.Database) {
	var (
		config *params.ChainConfig
		err    error
	)
	chainDb = MakeChainDatabase(ctx, stack)

	if useExist {
		stored := rawdb.ReadCanonicalHash(chainDb, 0)
		if (stored == common.Hash{}) {
			Fatalf("No existing genesis")
		}
		config = rawdb.ReadChainConfig(chainDb, stored)
	} else {
		config, _, err = core.SetupGenesisBlock(chainDb, MakeGenesis(ctx))
		if err != nil {
			Fatalf("%v", err)
		}
	}

	var engine consensus.Engine
	if config.Clique != nil {
		engine = clique.New(config.Clique, chainDb)
	} else if config.Istanbul != nil {
		// for IBFT
		istanbulConfig := istanbul.DefaultConfig
		if config.Istanbul.Epoch != 0 {
			istanbulConfig.Epoch = config.Istanbul.Epoch
		}
		istanbulConfig.ProposerPolicy = istanbul.ProposerPolicy(config.Istanbul.ProposerPolicy)
		istanbulConfig.Ceil2Nby3Block = config.Istanbul.Ceil2Nby3Block
		engine = istanbulBackend.New(istanbulConfig, stack.GetNodeKey(), chainDb)
	} else if config.IsQuorum {
		// for Raft
		engine = ethash.NewFullFaker()
	} else {
		engine = ethash.NewFaker()
		if !ctx.GlobalBool(FakePoWFlag.Name) {
			engine = ethash.New(ethash.Config{
				CacheDir:       stack.ResolvePath(eth.DefaultConfig.Ethash.CacheDir),
				CachesInMem:    eth.DefaultConfig.Ethash.CachesInMem,
				CachesOnDisk:   eth.DefaultConfig.Ethash.CachesOnDisk,
				DatasetDir:     stack.ResolvePath(eth.DefaultConfig.Ethash.DatasetDir),
				DatasetsInMem:  eth.DefaultConfig.Ethash.DatasetsInMem,
				DatasetsOnDisk: eth.DefaultConfig.Ethash.DatasetsOnDisk,
			}, nil, false)
		}
	}
	if gcmode := ctx.GlobalString(GCModeFlag.Name); gcmode != "full" && gcmode != "archive" {
		Fatalf("--%s must be either 'full' or 'archive'", GCModeFlag.Name)
	}
	cache := &core.CacheConfig{
		TrieCleanLimit:      eth.DefaultConfig.TrieCleanCache,
		TrieCleanNoPrefetch: ctx.GlobalBool(CacheNoPrefetchFlag.Name),
		TrieDirtyLimit:      eth.DefaultConfig.TrieDirtyCache,
		TrieDirtyDisabled:   ctx.GlobalString(GCModeFlag.Name) == "archive",
		TrieTimeLimit:       eth.DefaultConfig.TrieTimeout,
	}
	if ctx.GlobalIsSet(CacheFlag.Name) || ctx.GlobalIsSet(CacheTrieFlag.Name) {
		cache.TrieCleanLimit = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheTrieFlag.Name) / 100
	}
	if ctx.GlobalIsSet(CacheFlag.Name) || ctx.GlobalIsSet(CacheGCFlag.Name) {
		cache.TrieDirtyLimit = ctx.GlobalInt(CacheFlag.Name) * ctx.GlobalInt(CacheGCFlag.Name) / 100
	}
	vmcfg := vm.Config{EnablePreimageRecording: ctx.GlobalBool(VMEnableDebugFlag.Name)}
	chain, err = core.NewBlockChain(chainDb, cache, config, engine, vmcfg, nil)
	if err != nil {
		Fatalf("Can't create BlockChain: %v", err)
	}
	return chain, chainDb
}

// MakeConsolePreloads retrieves the absolute paths for the console JavaScript
// scripts to preload before starting.
func MakeConsolePreloads(ctx *cli.Context) []string {
	// Skip preloading if there's nothing to preload
	if ctx.GlobalString(PreloadJSFlag.Name) == "" {
		return nil
	}
	// Otherwise resolve absolute paths and return them
	var preloads []string

	assets := ctx.GlobalString(JSpathFlag.Name)
	for _, file := range strings.Split(ctx.GlobalString(PreloadJSFlag.Name), ",") {
		preloads = append(preloads, common.AbsolutePath(assets, strings.TrimSpace(file)))
	}
	return preloads
}

// MigrateFlags sets the global flag from a local flag when it's set.
// This is a temporary function used for migrating old command/flags to the
// new format.
//
// e.g. geth account new --keystore /tmp/mykeystore --lightkdf
//
// is equivalent after calling this method with:
//
// geth --keystore /tmp/mykeystore --lightkdf account new
//
// This allows the use of the existing configuration functionality.
// When all flags are migrated this function can be removed and the existing
// configuration functionality must be changed that is uses local flags
func MigrateFlags(action func(ctx *cli.Context) error) func(*cli.Context) error {
	return func(ctx *cli.Context) error {
		for _, name := range ctx.FlagNames() {
			if ctx.IsSet(name) {
				ctx.GlobalSet(name, ctx.String(name))
			}
		}
		return action(ctx)
	}
}
