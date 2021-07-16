# IBFT parameters

## CLI options

### Block period

`--istanbul.blockperiod 1`

Setting the block period is used for how long blocks should be minted by the validators. It is also used for validation
of block times by all nodes, so should not be changed after deciding a value for the network.
The setting is a positive integer, and measures the minimum numbers of seconds before the next block is considered 
valid.

The default value is `1`.

### Request timeout

`--istanbul.requesttimeout 10000`

The request timeout is the timeout at which IBFT will seek to trigger a new round if the previous one did not complete.
This period increases are the timeout is hit more often. This parameter sets the minimum timeout in the case of normal 
operation and is measured in milliseconds.

The default value is `10000`.

## Genesis file options

Within the `genesis.json` file, there is an area for IBFT specific configuration, much like a Clique network 
configuration. 

The options are as follows: 
```
{
    "config": {
        "istanbul": {
            "epoch": 30000,
            "policy": 0,
            "ceil2Nby3Block": 0,
            "qbftBlock": 0
        },
        ...
    },
    ...
}
```

### Epoch

The epoch specifies the number of blocks that should pass before pending validator votes are reset. When the
`blocknumber%EPOCH == 0`, the votes are reset in order to prevent a single vote from becoming stale. If the existing 
vote was still due to take place, then it must be resubmitted, along with all its votes.

### Policy

The policy refers to the proposer selection policy, which is either `ROUND_ROBIN` or `STICKY`.

A value of `0` denotes a `ROUND_ROBIN` policy, where the next expected proposer is the next in queue. Once a proposer 
has submitted a valid block, they join the back of the queue and must wait their turn again.

A value of `1` denotes a `STICKY` proposer policy, where a single proposer is selected to mint blocks and does so until
such a time as they go offline or are otherwise unreachable.

### ceil2Nby3Block

The `ceil2Nby3Block` sets the block number from which to use an updated formula for calculating the number of faulty 
nodes. This was introduced to enable existing network the ability to upgrade at a point in the future of the network, as
it is incompatible with the existing formula. For new networks, it is recommended to set this value to `0` to use the 
updated formula immediately.

To update this value, the same process can be followed as other hard-forks.

### qbftBlock

The `qbftBlock` sets the block number from which to use `qbft` consensus. This was introduced to enable existing ibft networks the ability to start using qbft consensus at a point in the future. For new networks, it is recommended to set this value to `0` to use the qbft consensus immediately.

To update this value, the same process can be followed as other hard-forks.