module github.com/ethereum/go-ethereum

go 1.15

// Quorum - Replace Go modules that use modifications done by us
replace (
	github.com/coreos/etcd => github.com/Consensys/etcd v3.3.13-quorum197+incompatible
	github.com/ethereum/go-ethereum/crypto/secp256k1 => ./crypto/secp256k1
)

// End Quorum

require (
	github.com/Azure/azure-pipeline-go v0.2.2 // indirect
	github.com/Azure/azure-storage-blob-go v0.7.0
	github.com/Azure/go-autorest/autorest/adal v0.8.0 // indirect
	github.com/BurntSushi/toml v0.3.1
	github.com/StackExchange/wmi v0.0.0-20180116203802-5d049714c4a6 // indirect
	github.com/VictoriaMetrics/fastcache v1.5.7
	github.com/aristanetworks/goarista v0.0.0-20170210015632-ea17b1a17847
	github.com/aws/aws-sdk-go v1.25.48
	github.com/btcsuite/btcd v0.0.0-20171128150713-2e60448ffcc6
	github.com/cespare/cp v0.1.0
	github.com/cloudflare/cloudflare-go v0.10.2-0.20190916151808-a80f83b9add9
	github.com/coreos/etcd v3.3.20+incompatible
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v0.0.0-20180603214616-504e848d77ea
	github.com/dlclark/regexp2 v1.2.0 // indirect
	github.com/docker/docker v1.4.2-0.20180625184442-8e610b2b55bf
	github.com/dop251/goja v0.0.0-20200721192441-a695b0cdd498
	github.com/eapache/channels v1.1.0
	github.com/eapache/queue v1.1.0 // indirect
	github.com/edsrzf/mmap-go v0.0.0-20160512033002-935e0e8a636c
	github.com/ethereum/go-ethereum/crypto/secp256k1 v0.0.0
	github.com/fatih/color v1.7.0
	github.com/fjl/memsize v0.0.0-20180418122429-ca190fb6ffbc
	github.com/gballet/go-libpcsclite v0.0.0-20190607065134-2772fd86a8ff
	github.com/go-ole/go-ole v1.2.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.2+incompatible // indirect
	github.com/go-stack/stack v1.8.0
	github.com/golang/mock v1.4.3
	github.com/golang/protobuf v1.3.4
	github.com/golang/snappy v0.0.3-0.20201103224600-674baa8c7fc3
	github.com/google/gofuzz v1.1.1-0.20200604201612-c04b05f3adfa
	github.com/gorilla/websocket v1.4.1-0.20190629185528-ae1634f6a989
	github.com/graph-gophers/graphql-go v0.0.0-20191115155744-f33e81362277
	github.com/hashicorp/go-hclog v0.13.0
	github.com/hashicorp/go-plugin v1.2.2
	github.com/hashicorp/golang-lru v0.5.4
	github.com/holiman/uint256 v1.1.1
	github.com/huin/goupnp v1.0.0
	github.com/influxdata/influxdb v1.2.3-0.20180221223340-01288bdb0883
	github.com/jackpal/go-nat-pmp v1.0.2-0.20160603034137-1fa385a6f458
	github.com/jedisct1/go-minisign v0.0.0-20190909160543-45766022959e
	github.com/jpmorganchase/quorum-account-plugin-sdk-go v0.0.0-20200714175524-662195b38a5e
	github.com/jpmorganchase/quorum-hello-world-plugin-sdk-go v0.0.0-20200210211148-57f99f69eeb3
	github.com/jpmorganchase/quorum-security-plugin-sdk-go v0.0.0-20200714173835-22a319bb78ce
	github.com/julienschmidt/httprouter v1.1.1-0.20170430222011-975b5c4c7c21
	github.com/karalabe/usb v0.0.0-20190919080040-51dc0efba356
	github.com/kr/pretty v0.1.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.4
	github.com/mattn/go-isatty v0.0.10
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/naoina/toml v0.1.2-0.20170918210437-9fafd6967416
	github.com/olekukonko/tablewriter v0.0.2-0.20190409134802-7e037d187b0c
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v0.0.0-20170112150404-1b00554d8222
	github.com/peterh/liner v1.1.1-0.20190123174540-a2c9a5303de7
	github.com/prometheus/tsdb v0.6.2-0.20190402121629-4f204dcbc150
	github.com/rjeczalik/notify v0.9.1
	github.com/rs/cors v0.0.0-20160617231935-a62a804a8a00
	github.com/rs/xhandler v0.0.0-20160618193221-ed27b6fd6521 // indirect
	github.com/shirou/gopsutil v2.20.5+incompatible
	github.com/status-im/keycard-go v0.0.0-20190316090335-8537d3370df4
	github.com/steakknife/bloomfilter v0.0.0-20180922174646-6819c0d2a570
	github.com/steakknife/hamming v0.0.0-20180906055917-c99c65617cd3 // indirect
	github.com/stretchr/testify v1.4.0
	github.com/syndtr/goleveldb v1.0.1-0.20190923125748-758128399b1d
	github.com/tv42/httpunix v0.0.0-20191220191345-2ba4b9c3382c
	github.com/tyler-smith/go-bip39 v1.0.1-0.20181017060643-dbb3b84ba2ef
	github.com/wsddn/go-ecdh v0.0.0-20161211032359-48726bab9208
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/net v0.0.0-20200822124328-c89045814202 // indirect
	golang.org/x/sys v0.0.0-20200824131525-c12d262b63d8
	golang.org/x/text v0.3.3
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	google.golang.org/grpc v1.29.1
	gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127
	gopkg.in/karalabe/cookiejar.v2 v2.0.0-20150724131613-8dcd6a7f4951
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce
	gopkg.in/olebedev/go-duktape.v3 v3.0.0-20190213234257-ec84240a7772
	gopkg.in/oleiade/lane.v1 v1.0.0
	gopkg.in/urfave/cli.v1 v1.20.0
	gotest.tools v2.2.0+incompatible // indirect
)
