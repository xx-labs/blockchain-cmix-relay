package cmd

import (
	"fmt"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
)

// ---------------------------- //
// Network represents a single restlike endpoint
// with a given URI for querying a blockchain network
// Examples:
//
//	bitcoin/mainnet
//	ethereum/mainnet
//	ethereum/goerli
//
// Multiple endpoints can be configured in order to
// load balance requests
type Network struct {
	uri       string
	endpoints []string
}

// Configuration for a single network
type NetworkConfig struct {
	Name      string   `mapstructure:"name"`
	Endpoints []string `mapstructure:"endpoints"`
}

// ---------------------------- //
// Constructor
func NewNetwork(uri string, endpoints []string) *Network {
	return &Network{
		uri,
		endpoints,
	}
}

// ---------------------------- //
// This is the callback function called by xxDK in order
// to process a restlike request
// This function will randomly choose one of the configured
// blockchain endpoints, perform the query, and return the response
// which is then sent back to the client over the cMix network
func (n *Network) Callback(request *restlike.Message) *restlike.Message {
	jww.DEBUG.Printf("[RELAY %v] Request received over cMix: %v", n.uri, request)
	if n.uri != "custom" && request.Uri != n.uri {
		jww.WARN.Printf("[RELAY %v] Received URI (%v) doesn't match for this query!", n.uri, request.Uri)
	}

	// Response
	response := &restlike.Message{}
	response.Headers = &restlike.Headers{}
	response.Content = nil
	response.Error = ""

	endpoints := n.endpoints
	// Check content is not empty
	if len(request.Content) == 0 {
		jww.DEBUG.Printf("[RELAY %v] Got empty request", n.uri)
		response.Error = "Request content cannot be empty"
	} else {
		// If this is custom URI get the endpoint from request headers
		if n.uri == "custom" {
			endpoint := getEndpointFromHeaders(request.Headers)
			if endpoint == "" {
				jww.INFO.Printf("[RELAY %v] Couldn't get a valid endpoint URL from request Headers: %v", n.uri, request.Headers)
				response.Error = "Request doesn't have a valid custom endpoint URL in request Headers"
			} else {
				// Test endpoint connection
				if !testConnectJsonRpc(endpoint) {
					jww.INFO.Printf("[RELAY %v] Couldn't connect to custom endpoint URL", n.uri)
					response.Error = "Provided custom endpoint URL is unreachable"
				} else {
					endpoints = []string{endpoint}
				}
			}
		}
	}

	if response.Error == "" {
		// Do JSON-RPC query
		data, err := doQuery(endpoints, request.Content)
		if err != nil {
			errMsg := fmt.Sprintf("Error in JSON-RPC query: %v", err)
			jww.ERROR.Printf("[RELAY %v] %s", n.uri, errMsg)
			response.Error = errMsg
		} else {
			response.Content = data
			jww.DEBUG.Printf("[RELAY %v] Response: %v", n.uri, string(data))
		}
	}
	return response
}
