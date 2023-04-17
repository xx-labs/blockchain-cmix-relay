# blockchain-cmix-relay

This repo contains the implementation of a pair of client / server applications that communicate over the cMix network.
The first is a relay server that receives JSON-RPC requests over cmix and routes them to configured blockchain RPC endpoints.
The second is it's client, that opens a local HTTP proxy server and sends JSON-RPC requests over cMix to the relay server.

## Using with MetaMask

The xx labs team is working on a desktop application called `Proxxy` that makes use of the `client` library from this repository to bring privacy to interactions with multiple EVM compatible blockchains (specifically geared towards MetaMask users).

While this is not complete, the command line `client` application from this repository is the only way to communicate with a `relay` server.

The xx labs teams has deployed a relay server for testing purposes, with support for various testnets. The `relay` contact file can be found in this repository [here](relay.xxc).

Furthermore, anyone can setup their own relay server by getting RPC endpoints to desired networks with their chosen RPC service provider(s), and then following the instructions in this README.

In either scenario, once a relay server is online, the next steps show how to use the `client` to send privacy enhanced transactions with MetaMask.

### Instructions

1. Get the `relay.xxc` contact file for the relay server. The instructions assume usage of the Ethereum Goerli testnet, so make sure this relay server supports that network.

2. Compile and start the client application (check below for how to do this). By default it opens a local HTTP proxy server on `http://localhost:9296`.

3. Open the MetaMask extension on your browser.

4. Add the custom network with the following parameters
```
Network name: xx network ETH Goerli
New RPC URL: http://localhost:9296/ethereum/goerli
Chain ID: 5
Currency symbol: GoerliETH
```

5. To further enhance your privacy in MetaMask, go to Settings -> Security & Privacy and disable:
* `Show balance and token price checker`
* `Show incoming transactions`
* `Autodetect tokens`
* `Batch account balance requests`

Disabling this settings will make your MetaMask experience feel worse, but it will ensure that no requests are made to explorers such as etherscan that could potentially leak your metadata (IP address, etc.)

Please note that MetaMask could feel quite slow, due to the amount of JSON-RPC requests that it makes by default, which all have to go through round-trips of sending/receiving over the cMix network.

## How to compile

### Relay
```sh
cd relay
go build -o relay
```

### Client
```sh
cd client
go build -o client
```

## Relay Usage

The relay server requires a fixed cMix receiving identity, so that it can be reached by any clients. It expects the xxDK state to exist, so on the first time running the relay it is necessart to generate a fresh identity. This can be done with the following command:
```sh
./relay init -p [password used to encrypt xxDK state here] -c ../mainnet.crt
```

Then to run the server (in background):
```sh
./relay -p [password used to encrypt xxDK state here] &
```

Supported blockchain networks are loaded, by default, from the configuration file `networks.json`.
An example of this JSON configuration file can be found [here](relay/networks-example.json).
The networks configuration file can be changed while the relay server is running, supported networks will be automatically reloaded.

The default log file is `relay.log` and relevant logs have the prefix `[RELAY]`. Watch the logs with
```sh
tail -F relay.log | grep "RELAY"
```

To see all configuration flags
```sh
./relay -h

Usage:
  relay [flags]
  relay [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  init        Initialize the REST server

Flags:
  -h, --help                   help for relay
  -f, --logFile string         Path to log file (default "relay.log")
  -l, --logLevel uint          Level of debugging to print (0 = info, 1 = debug, >1 = trace).
      --logPrefix string       Logging prefix (default "RELAY")
  -n, --networks string        Path to networks configuration file (default "networks.json")
  -p, --statePassword string   Password for cMix state
  -s, --statePath string       Path cMix state directory (default "state")

Use "relay [command] --help" for more information about a command.
```

## Client Usage

The client automatically generates a cMix identity on startup, unless xxDK state already exists, in which case it will reuse it. Reusing the xxDK state doesn't reduce privacy, since all requests use the single use feature of xxDK, meaning that each individual request sent over cMix has an unique, ephemeral reception identity.

The client requires the relay server contact file `relay.xxc` to be placed in the same directory where it runs. Alternatively, the path to the contact file can be specified via a config flag (see list of flags below).

To run the client (in background):
```sh
./client -p [password used to encrypt xxDK state here] -r ../mainnet.crt &
```

The default log file is `client.log` and relevant logs have the prefix `[RELAY]`. Watch the logs with
```sh
tail -F client.log | grep "RELAY"
```

Upon startup, the `client` will connect to the cMix network and wait for this connection to be healthy. Then it will contact the `relay` server and request its supported networks. These can be found in the logs, for example:
```sh
> ./client -p 1234 & ; tail -F client.log | grep "RELAY"

INFO 2023/04/16 20:51:58 [RELAY] Loading state at state
INFO 2023/04/16 20:52:01 [RELAY] Started cMix Client
INFO 2023/04/16 20:52:01 [RELAY] Sending cMix request with content:
INFO 2023/04/16 20:52:13 [RELAY] Supported networks
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/ethereum/goerli
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/ethereum/sepolia
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/avalanche/fuji/c
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/polygon/mumbai
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/fantom/testnet
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/aurora/testnet
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/celo/alfajores
INFO 2023/04/16 20:52:13 [RELAY] http://localhost:9296/custom
INFO 2023/04/16 20:52:13 [RELAY] Starting HTTP server on port: 9296
```

To see all configuration flags
```sh
./client -h

Usage:
  client [flags]

Flags:
  -r, --cert string            Path to certificate file used to verify NDF download (default "mainnet.crt")
  -c, --contactFile string     Path to file containing the REST server contact info (default "relay.xxc")
  -h, --help                   help for client
  -f, --logFile string         Path to log file (default "client.log")
  -l, --logLevel uint          Level of debugging to print (0 = info, 1 = debug, >1 = trace).
      --logPrefix string       Logging prefix (default "RELAY")
  -d, --ndf string             URL used to download NDF file on initialization (default "https://elixxir-bins.s3.us-west-1.amazonaws.com/ndf/mainnet.json")
  -t, --port int               Port to listen on for local HTTP proxy server (default 9296)
  -n, --retries int            How many times to retry sending request over cMix (default 3)
  -p, --statePassword string   Password for cMix state
  -s, --statePath string       Path cMix state directory (default "state")
```
