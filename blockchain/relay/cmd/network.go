package cmd

import (
	"encoding/binary"
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
	metrics   *Metrics
}

// Configuration for a single network
type NetworkConfig struct {
	Name      string   `mapstructure:"name"`
	Endpoints []string `mapstructure:"endpoints"`
}

// ---------------------------- //
// Constructor
func NewNetwork(uri string, endpoints []string) *Network {
	kind := MetricsKindGeneric
	if uri == "/custom" {
		kind = MetricsKindCustom
	}
	return &Network{
		uri:       uri,
		endpoints: endpoints,
		metrics:   NewMetrics(uri, kind),
	}
}

// ---------------------------- //
// This is the callback function called by xxDK in order
// to process a restlike request
// This function will randomly choose one of the configured
// blockchain endpoints, perform the query, and return the response
// which is then sent back to the client over the cMix network
func (n *Network) Callback(request *restlike.Message) *restlike.Message {
	jww.INFO.Printf("[%s %s] Request received over cMix: %v", logPrefix, n.uri, request)
	n.metrics.IncTotal()
	if request.Uri != n.uri {
		jww.WARN.Printf("[%s %s] Received URI (%v) doesn't match for this query!", logPrefix, n.uri, request.Uri)
	}

	// Response
	response := &restlike.Message{}
	// Start with code 400 (Bad Request)
	code := 400
	response.Headers = &restlike.Headers{Headers: make([]byte, 2)}
	response.Content = nil
	response.Error = ""

	endpoints := n.endpoints
	// Check content is not empty
	if len(request.Content) == 0 {
		jww.WARN.Printf("[%s %s] Got empty request", logPrefix, n.uri)
		response.Error = "Request content cannot be empty"
		n.metrics.IncFailedEmpty()
	} else {
		// If this is custom URI get the endpoint from request headers
		if n.uri == "/custom" {
			endpoint := getEndpointFromHeaders(request.Headers)
			if endpoint == "" {
				jww.WARN.Printf("[%s %s] Couldn't get a valid endpoint URL from request Headers: %v", logPrefix, n.uri, request.Headers)
				response.Error = "Request doesn't have a valid custom endpoint URL in request Headers"
				n.metrics.IncFailedInvalidUrl()
			} else {
				// Test endpoint connection
				if !testConnectJsonRpc(endpoint) {
					jww.WARN.Printf("[%s %s] Couldn't connect to custom endpoint URL", logPrefix, n.uri)
					response.Error = "Provided custom endpoint URL is unreachable"
					n.metrics.IncFailedUnreachableUrl()
				} else {
					endpoints = []string{endpoint}
				}
			}
		}
	}

	if response.Error == "" {
		// Do JSON-RPC query
		var data []byte
		var err error
		data, code, err = doQuery(endpoints, request.Content)
		if err != nil {
			errMsg := fmt.Sprintf("Error in JSON-RPC query: %v", err)
			jww.WARN.Printf("[%s %s] %s", logPrefix, n.uri, errMsg)
			response.Error = errMsg
			n.metrics.IncFailedRpc()
		} else {
			response.Content = data
			jww.INFO.Printf("[%s %s] Code (%v), Response: %v", logPrefix, n.uri, code, string(data))
		}
	}
	// Place response code in headers
	binary.LittleEndian.PutUint16(response.Headers.Headers, uint16(code))
	n.metrics.IncSuccessful()
	return response
}
