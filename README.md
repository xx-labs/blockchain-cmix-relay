# blockchain-cmix-relay

This repo contains the implementation of a pair of client / server applications that communicate over the cMix network.
The first is a relay server that receives JSON-RPC requests over cmix and routes them to configured blockchain RPC endpoints.
The second is it's client, that opens a local HTTP proxy server and sends JSON-RPC requests over cMix to the relay server.

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

The relay server requires a fixed cMix receiving identity, so that it can be reached by any clients.
To generate a fresh identity:
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

The client automatically generates a fresh cMix identity on startup. Furthermore, all requests use the single use feature of xxDK, meaning that each individual request sent over cMix has an unique, ephemeral reception identity.

To run the client (in background):
```sh
./relay -p [password used to encrypt xxDK state here] -r ../mainnet.crt &
```

The default log file is `client.log` and relevant logs have the prefix `[RELAY]`. Watch the logs with
```sh
tail -F client.log | grep "RELAY"
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
  -d, --ndf string             URL used to download NDF file on initialization (default "https://elixxir-bins.s3.us-west-1.amazonaws.com/ndf/mainnet.json")
  -t, --port int               Port to listen on for local HTTP proxy server (default 9296)
  -n, --retries int            How many times to retry sending request over cMix (default 3)
  -p, --statePassword string   Password for cMix state
  -s, --statePath string       Path cMix state directory (default "state")
```

## Using with existing wallets

The xx labs team has deployed a relay server for testing purposes.
It currently supports the Ethereum Goerli testnet.

In order to communicate with this server, ask for the contact file on the official xx network Discord [server](https://discord.com/invite/Y8pCkbK).

Then, compile and start the client application. By default it opens a local HTTP proxy server on `http://localhost:9296`.

Finally, navigate to your favorite wallet that supports adding custom networks, for example, MetaMask.

Add the custom network with the following parameters
```
Network name: xx network
New RPC URL: http://localhost:9296/ethereum/goerli
Chain ID: 5
Currency symbol: GoerliETH
```

To maintain privacy, go to Settings -> Security & Privacy and disable `Show balance and token price checker`, `Show incoming transactions`, `Autodetect tokens` and `Batch account balance requests`.

Please note that MetaMask could feel quite slow, due to the amount of JSON-RPC requests that it makes by default, which all have to go through round-trips of sending/receiving over the cMix network.
