title: Plugin Development - Pluggable Architecture - Quorum

We leverage HashiCorp's [`go-plugin`](https://github.com/hashicorp/go-plugin) to enable our plugin-based architecture using gRPC.

We recommend reading the [`go-plugin` gRPC examples](https://github.com/hashicorp/go-plugin/tree/master/examples/grpc).
Some advanced topics which are not available in the `go-plugin` documentation will be covered here.

## Life Cycle

A plugin is started as a separate process and communicates with the Quorum client host process via gRPC service interfaces.
This is done over a mutually-authenticated TLS connection on the local machine. The implementation is done inside `go-plugin`
library which benefits plugins written in Golang. For plugins written in other languages, plugin authors need to have
a understanding about the model as described below:

1. `geth` looks for the plugin distribution file after reading the plugin definition from settings
1. `geth` verifies the plugin distribution file integrity
1. `geth` generates a self-signed certificate (aka client certificate)
1. `geth` spawns the plugin with the client certificate
1. The plugin imports the client certificate and generates a self-signed server certificate for its RPC server
1. The plugin includes the RPC server certificate in the handshake
1. `geth` imports the plugin RPC server certificate
1. `geth` and the plugin communicate via RPC over TLS using mutual TLS

Each plugin must implement the [`PluginInitializer`](#plugininitializer) gRPC service interface.
After the plugin process is successfully started and connection with the Quorum client is successfully established,
Quorum Client invokes `Init()` gRPC in order to initialize the plugin with configuration data 
read from the plugin definition's `config` field in [settings](../Settings/#plugindefinition) file.

## Configuration Data

A plugin receives its [configuration data](#proto.PluginInitialization.Request) from Quorum Client via `Init()` gRPC. 

## Distribution

### File format

Plugin distribution file must be a ZIP file. File name format is `<name>-<version>.zip`. 
Where `<name>` and `<version>` are from plugin definition in [settings](../Settings/#plugindefinition) file.

### Metadata 

A plugin metadata file `plugin-meta.json` must be included in the distribution ZIP file.
`plugin-meta.json` contains a valid JSON object which has a flat structure with key value pairs.

Although the JSON object can include any desired information.
There are mandatory key value pairs which must be present. 

<pre>
{
    "name": string,
    "version": string,
    "entrypoint": string,
    "parameters": array(string),
    ...
}
</pre>

| Fields       | Description                                                        |
|:-------------|:-------------------------------------------------------------------|
| `name`       | (**Required**) Name of the plugin                                    |
| `version`    | (**Required**) Version of the plugin                                 |
| `entrypoint` | (**Required**) Command to execute the plugin process                 |
| `parameters` | (**Optional**) Command parameters to be passed to the plugin process |

E.g.:
```json
{
  "name": "quorum-plugin-helloWorld",
  "version": "1.0.0",
  "entrypoint": "helloWorldPlugin"
}
```

## Advanced topics for non-Go plugins

Writing non-Go plugins is well-documented in [`go-plugin` Github](https://github.com/hashicorp/go-plugin/blob/master/docs/guide-plugin-write-non-go.md).
Some additional advanced topics are described here.

### Magic Cookie

Magic Cookie key and value are used as a very basic verification that a plugin is intended to be launched. 
This is not a security measure, just a UX feature. 

Magic Cookie key and value are injected as an environment variable while executing the plugin process.
Pre-defined magic cookie key and value to be used in a plugin can be found [here]().

If the magic cookie doesn't match, plugin should show human-friendly output.

### Mutual TLS Authentication

The Quorum client requires the plugin to authenticate and secure the connection via mutual TLS. 
`PLUGIN_CLIENT_CERT` environment variable is populated with Quorum Client certificate (in PEM format).
A plugin would need to include this certificate to its trusted certificate pool, then
generate a self-signed certificate and append the base64-encoded value of the certificate (in DER format)
in the [handshake](https://github.com/hashicorp/go-plugin/blob/master/docs/internals.md#handshake) message.

## Examples

Please visit [Overview](../Overview/#example-helloworld-plugin) page for a built-in HelloWorld plugin example.

<a name="plugininitializer"></a>

{!./PluggableArchitecture/Plugins/init_interface.md!}