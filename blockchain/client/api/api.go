package api

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"github.com/xx-labs/blockchain-cmix-relay/cmix"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/crypto/contact"
)

// ---------------------------- //
// Api wraps the cMix Client
// and performs requests
// to multiple Relay Servers
type Api struct {
	client    *cmix.Client
	logPrefix string
	retries   int
	relayers  map[string]*Relay
	active    map[string]bool
	mux       sync.RWMutex
}

// Configuration variables for the Api
type Config struct {
	// Cmix Config
	Cmix cmix.Config

	// Number of retries for each request
	Retries int

	// Server contact files
	ServerContacts []ServerInfo
}

type ServerInfo struct {
	ContactFile string
	Contact     contact.Contact
	Name        string
}

// ---------------------------- //
// Create a new API instance to
// access blockchains over cMix
// Input: the filepath of the server
// contact file
// Panics on failure to open and parse
// contact data
func NewApi(c Config) *Api {
	// Create cMix client
	client := cmix.NewClient(c.Cmix)

	// Create relay servers
	relayers := make(map[string]*Relay, len(c.ServerContacts))
	active := make(map[string]bool, len(c.ServerContacts))
	for _, contactInfo := range c.ServerContacts {
		contact := contactInfo.Contact
		// If contact file is provided load the contact from it instead
		if contactInfo.ContactFile != "" {
			contact = cmix.LoadContactFile(contactInfo.ContactFile)
		}
		relayers[contactInfo.Name] = NewRelay(contactInfo.Name, client, contact, c.Cmix.LogPrefix, c.Retries)
		active[contactInfo.Name] = false
	}

	return &Api{
		client:    client,
		logPrefix: c.Cmix.LogPrefix,
		retries:   c.Retries,
		relayers:  relayers,
		active:    active,
	}
}

// ---------------------------- //
// Connect the API to the REST server
// Starts cMix client
// Loads supported networks from server
// Returns an error if it can't connect
// to the server over cMix and get
// supported networks
func (a *Api) Connect() {
	// Start cMix client
	a.client.Start()

	// Start relayers
	for _, relayer := range a.relayers {
		relayer.Start(a.updateRelayers)
	}

	// Wait until at least one relayer is active
	for {
		relayers := a.activeRelayers()
		if len(relayers) > 0 {
			return
		}
		time.Sleep(1 * time.Second)
	}
}

// ---------------------------- //
// Disconnect the API
// Stops cMix client
// Clears supported networks
func (a *Api) Disconnect() {
	// Mark all relayers as not active to prevent new requests
	a.mux.Lock()
	for name := range a.active {
		a.active[name] = false
	}
	a.mux.Unlock()

	// Stop relayers
	wg := sync.WaitGroup{}
	for _, relayer := range a.relayers {
		wg.Add(1)
		go func(r *Relay) {
			r.Stop()
			wg.Done()
		}(relayer)
	}

	// Stop cMix Client
	a.client.Stop()

	// Wait for relayers to stop
	wg.Wait()
}

// ---------------------------- //
// Return list of supported networks
// NOTE: this list is loaded from each relay server
// on Api.Connect()
func (a *Api) Networks() []string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	networks := make([]string, 0)
	seen := make(map[string]struct{})
	for _, r := range a.relayers {
		nets := r.Networks()
		for _, net := range nets {
			if _, ok := seen[net]; !ok {
				networks = append(networks, net)
				seen[net] = struct{}{}
			}
		}
	}
	return networks
}

// ---------------------------- //
// Do a Request over cMix to the given network
// with the given data
// Returns response data, code and possible error
func (a *Api) Request(network string, data []byte) ([]byte, int, error) {
	return a.doRequest(restlike.Post, network, data)
}

// ---------------------------- //
// Internal functions
// ---------------------------- //

// callback to update active relayers
func (a *Api) updateRelayers(name string, active bool) {
	a.mux.Lock()
	defer a.mux.Unlock()
	a.active[name] = active
}

func (a *Api) activeRelayers() []*Relay {
	a.mux.RLock()
	defer a.mux.RUnlock()
	relayers := make([]*Relay, 0)
	for name, active := range a.active {
		if active {
			relayers = append(relayers, a.relayers[name])
		}
	}
	return relayers
}

// do a request over cMix
func (a *Api) doRequest(
	method restlike.Method,
	uri string,
	data []byte,
) (resp []byte, code int, err error) {
	// Parse URI
	endpoint := parseCustomUri(uri)
	var headers []byte = nil

	// If custom URI
	if endpoint != "" {
		// Place endpoint in headers
		headers = []byte(endpoint)
		// Change URI to just "custom"
		uri = "/custom"
	}

	// Get active relayers
	relayers := a.activeRelayers()

	if len(relayers) == 0 {
		jww.ERROR.Printf("[%s] No active relayers!", a.logPrefix)
		return nil, 500, errors.New("relayers not active")
	}

	// Make sure the network is supported
	useRelayers := make([]*Relay, 0)
	for _, r := range relayers {
		if r.SupportsNetwork(uri) {
			useRelayers = append(useRelayers, r)
		}
	}
	if len(useRelayers) == 0 {
		jww.ERROR.Printf("[%s] Network %v is not supported", a.logPrefix, uri)
		return nil, 400, errors.New("unsupported network")
	}

	// Build request
	request := cmix.Request{
		Method:  method,
		Uri:     uri,
		Data:    data,
		Headers: headers,
	}

	// Do request over cMix
	// Repeat for number of retries choosing a different relay server if possible
	tries := 0
	if len(useRelayers) > 1 {
		shuffle(useRelayers)
	}
	err = errors.New("dummy")
	for err != nil {
		// Choose a different relay server
		idx := tries % len(useRelayers)
		resp, code, err = useRelayers[idx].Request(request)
		tries++
		if tries >= a.retries {
			break
		}
	}

	// Bail if can't do request in specified number of retries
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to send request after %v retries, bailing", a.logPrefix, a.retries)
		return nil, 500, errors.New("request exhausted number of retries")
	}

	return resp, code, nil
}

// Parse custom URI
// Extract the endpoint URL from the URI
func parseCustomUri(uri string) string {
	endpoint := ""
	parts := strings.SplitN(uri, "/", 3)
	if len(parts) > 2 && parts[1] == "custom" {
		endpoint = parts[2]
	}
	return endpoint
}

// Shuffle slice of relayers
func shuffle(relayers []*Relay) {
	// Get the length of the slice
	n := len(relayers)

	// Initialize a random number generator with a seed based on the current time
	rand.Seed(time.Now().UnixNano())

	// Loop through the slice from the end to the beginning
	for i := n - 1; i >= 1; i-- {
		// Generate a random index j between 0 and i
		j := rand.Intn(i + 1)

		// Swap the elements at index i and j
		relayers[i], relayers[j] = relayers[j], relayers[i]
	}
}
