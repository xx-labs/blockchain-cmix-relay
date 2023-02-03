package cmd

import (
	"encoding/json"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
)

// ---------------------------- //
// Manager encapsulates all the supported networks
type Manager struct {
	uri       string
	networks  []*Network
	endpoints *restlike.Endpoints
}

// ---------------------------- //
// Constructor

// Creates the network manager and
// registers server endpoints with xxDK
// for all supported networks
func NewManager(
	networks map[string][]NetworkConfig,
	endpoints *restlike.Endpoints,
) *Manager {
	// Create Manager
	m := &Manager{
		uri:       "/networks",
		endpoints: endpoints,
	}
	// Initialize networks
	m.initNetworks(networks)
	return m
}

// ---------------------------- //
// Reload a manager
// Remove all endpoints first and
// then destroy networks
// Finally, initialize new supported networks
func (m *Manager) Reload(networks map[string][]NetworkConfig) {

	// Remove supported networks endpoint
	m.endpoints.Remove(restlike.URI(m.uri), restlike.Get)

	// Remove all networks
	for idx, net := range m.networks {
		// Remove endpoint
		m.endpoints.Remove(restlike.URI(net.uri), restlike.Post)
		// Clear network
		net.endpoints = nil
		m.networks[idx] = nil
	}
	m.networks = nil

	// Initialize new networks
	m.initNetworks(networks)
}

// ---------------------------- //
// This is the callback function called by xxDK in order
// to process a restlike request
// This function returns a list of the supported networks
func (m *Manager) Callback(request *restlike.Message) *restlike.Message {
	jww.INFO.Printf("[%s %s] Request received over cMix: %v", logPrefix, m.uri, request)
	if request.Uri != m.uri {
		jww.WARN.Printf("[%s %s] Received URI (%v) doesn't match for this query!", logPrefix, m.uri, request.Uri)
	}

	// Response
	response := &restlike.Message{}
	response.Headers = &restlike.Headers{}
	response.Content = nil

	// Get list of supported networks URIs
	networks := make([]string, len(m.networks))
	for idx, net := range m.networks {
		networks[idx] = net.uri
	}

	// Convert to JSON data
	data, err := json.Marshal(networks)
	if err != nil {
		jww.ERROR.Printf("[%s %s] Error marshalling JSON data: %v", logPrefix, m.uri, err)
		response.Error = "Internal server error"
	} else {
		jww.INFO.Printf("[%s %s] Response: %v", logPrefix, m.uri, string(data))
		response.Content = data
	}
	return response
}

// ---------------------------- //
// Internal functions
// ---------------------------- //

func (m *Manager) initNetworks(networks map[string][]NetworkConfig) {
	m.networks = make([]*Network, 0, len(networks))
	// Create network representation for each
	// supported network
	for net, subnets := range networks {
		for _, n := range subnets {
			uri := "/" + net + "/" + n.Name
			// Test endpoints
			endpoints := make([]string, 0, len(n.Endpoints))
			for _, url := range n.Endpoints {
				if testConnectJsonRpc(url) {
					endpoints = append(endpoints, url)
				} else {
					jww.INFO.Printf("[%s] Network %v endpoint %v is unreachable, will be ignored", logPrefix, uri, url)
				}
			}
			if len(endpoints) == 0 {
				jww.WARN.Printf("[%s] Network %v has no valid endpoints, not supporting this network!", logPrefix, uri)
			} else {
				network := NewNetwork(uri, endpoints)
				m.networks = append(m.networks, network)
				jww.INFO.Printf("[%s] Creating network: %v", logPrefix, uri)
				m.endpoints.Add(restlike.URI(uri), restlike.Post, network.Callback)
			}
		}
	}

	// Add custom network
	custom := NewNetwork("/custom", []string{})
	m.networks = append(m.networks, custom)
	jww.INFO.Printf("[%s] Creating network: /custom", logPrefix)
	m.endpoints.Add(restlike.URI("/custom"), restlike.Post, custom.Callback)

	// Register manager endpoint to get supported networks
	jww.INFO.Printf("[%s] Creating endpoint: /networks", logPrefix)
	m.endpoints.Add(restlike.URI(m.uri), restlike.Get, m.Callback)
}
