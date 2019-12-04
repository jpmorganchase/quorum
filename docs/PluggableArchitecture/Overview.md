title: Overview - Pluggable Architecture - Quorum

The Quorum client is a modified `geth` client. One of the unique enhancements
is the pluggable architecture which allows adding additional features as plugins to the core `geth`, 
providing extensibility, flexibility, and isolation of Quorum features.

## Benefits
 
This enhancement provides a number of benefits, including:

1. Allowing the implementation of certain components of the Quorum client to be changed at configuration time.
1. Supporting our community to improve the Quorum client with their own innovative implementations of the supported pluggable components.
1. Decoupling new Quorum-specific features from core `geth` thereby simplifying the process of pulling in changes from upstream `geth`.

## How it works?

Each plugin exposes an implementation for a specific [plugin interface](https://github.com/jpmorganchase/quorum-plugin-definitions) (or see `Pluggable Architecture -> Plugins` for more details)
Plugins are executed as a separate process and communicate with the main Quorum client `geth` process
over a [gRPC](https://grpc.io/) interface.

The plugin implementation must adhere to certain gRPC services defined in a `.proto` file corresponding to the plugin interface.
Plugins can be written in different languages as gRPC provides a mechanism to generate stub code from `.proto` files. 

The network communication and RPC are handled automatically by the [high-level plugin library](https://github.com/hashicorp/go-plugin).

## Installing Plugins

Currently plugins must be manually installed into a directory (defaults to `plugins` directory inside `geth` data directory - default can be overriden by setting `baseDir` in [plugins settings](../Settings)).
 
## Using Plugins

[Plugins settings file](../Settings) contains a JSON that describes what plugins to be used.
Then start `geth` with `--plugins` as below:

```bash
geth ... \
     --plugins file:///<path>/<to>/plugins.json
```

## Plugin Integrity Verification
In its default settings Quorum uses its own Plugin Central Server to download and verify plugin integrity using [PGP](https://en.wikipedia.org/wiki/Pretty_Good_Privacy). 
However the architecture enables the same verification process locally via `--plugins.localverify` and `--plugins.publickey` flags or 
remotely with custom plugin central - reference the [`Settings`](../Settings/) section for more information on how to support custom plugin central. 

If the flag `--plugins.skipverify` is provided at runtime the plugin verification process will be disabled.

!!! warning
    Using `--plugins.skipverify`  is not advised for production settings and it should be avoided as it introduces security risks.

## Example: `HelloWorld` plugin

In this example, `HelloWorld` plugin exposes a JSON RPC endpoint to return a greeting message in the configured language.
This plugin is [reloadable](../Internals/#plugin-reloading).

The `HelloWorld` example plugin is included in the [Quorum GitHub repository](https://github.com/jpmorganchase/quorum/) for demo purposes. 
Usually, the source code for a plugin will be maintained in a separate repository.

Prequisites to run this example are similar to those for building `geth` with `make`

1. Clone Quorum repository
   ```bash
   › git clone https://github.com/jpmorganchase/quorum.git
   › cd quorum
   ```
1. Build `geth` and the plugin
   ```bash
   quorum› make geth helloWorldPlugin
   ```
   Notice that there are files being created under `build/bin` directory:
    - `geth-plugin-settings.json` which is the plugin settings file for `geth`
    - `quorum-plugin-helloWorld-1.0.0.zip` which is the `HelloWorld` plugin distribution zip file
    - `helloWorld-plugin-config.json` which is the config file for the plugin
1. Run `geth` with plugin
   ```bash
   quorum› PRIVATE_CONFIG=ignore \
   build/bin/geth \
        --nodiscover \
        --verbosity 5 \
        --networkid 10 \
        --raft \
        --raftjoinexisting 1 \
        --datadir ./build/_workspace/test \
        --rpc \
        --rpcapi eth,debug,admin,net,web3,plugin@helloworld \
        --plugins file://./build/bin/geth-plugin-settings.json \
        --plugins.skipverify
   ```
   `ps -ef | grep helloWorld` would reveal the `HelloWorld` plugin process
1. Call the JSON RPC
   ```bash
   quorum› curl -X POST http://localhost:8545 \
        -H "Content-type: application/json" \
        --data '{"jsonrpc":"2.0","method":"plugin@helloworld_greeting","params":["Quorum Plugin"],"id":1}'
   {"jsonrpc":"2.0","id":1,"result":"Hello Quorum Plugin!"}
   ```
1. Update plugin config to support `es` language
   ```bash
   # update language to "es"
   quorum› vi build/bin/helloWorld-plugin-config.json
   ```
1. Reload the plugin
   ```bash
   quorum› curl -X POST http://localhost:8545 \
        -H "Content-type: application/json" \
        --data '{"jsonrpc":"2.0","method":"admin_reloadPlugin","params":["helloworld"],"id":1}'
   {"jsonrpc":"2.0","id":1,"result":true}
   ```
1. Call the JSON RPC
   ```bash
   quorum› curl -X POST http://localhost:8545 \
        -H "Content-type: application/json" \
        --data '{"jsonrpc":"2.0","method":"plugin@helloworld_greeting","params":["Quorum Plugin"],"id":1}'
   {"jsonrpc":"2.0","id":1,"result":"Hola Quorum Plugin!"}
   ```