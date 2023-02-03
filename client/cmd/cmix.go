package cmd

import (
	"os"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
	restSingle "gitlab.com/elixxir/client/v4/restlike/single"
	"gitlab.com/elixxir/client/v4/single"
	"gitlab.com/elixxir/client/v4/xxdk"
	"gitlab.com/elixxir/crypto/contact"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/crypto/fastRNG"
)

// ---------------------------- //
// Client holds the xxDK user info
type Client struct {
	user   *xxdk.E2e
	stream *fastRNG.Stream
	grp    *cyclic.Group
}

// ---------------------------- //
// Create a new cMix client
func NewClient() *Client {
	// Initialize xxDK state
	// Always overwrite existing state to get fresh identities
	_, err := os.Stat(statePath)
	if err == nil {
		jww.INFO.Printf("[%s] Removing existing state at %v", logPrefix, statePath)
		err = os.RemoveAll(statePath)
		if err != nil {
			jww.FATAL.Panicf("[%s] Error removing existing state at %v", logPrefix, statePath)
		}
	}
	jww.INFO.Printf("[%s] Initializing state at %v", logPrefix, statePath)
	// Retrieve NDF
	cert, err := os.ReadFile(cert)
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to read certificate: %v", logPrefix, err)
	}

	ndfJSON, err := xxdk.DownloadAndVerifySignedNdfWithUrl(ndfUrl, string(cert))
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to download NDF: %+v", logPrefix, err)
	}

	// Initialize the state using the state file
	err = xxdk.NewCmix(string(ndfJSON), statePath, []byte(statePassword), "")
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to initialize state: %+v", logPrefix, err)
	}

	// Load cMix
	net, err := xxdk.LoadCmix(statePath, []byte(statePassword),
		xxdk.GetDefaultCMixParams())
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to load state: %+v", logPrefix, err)
	}

	// Get reception identity (automatically created if one does not exist)
	identityStorageKey := "identityStorageKey"
	identity, err := xxdk.LoadReceptionIdentity(identityStorageKey, net)
	if err != nil {
		// If no extant xxdk.ReceptionIdentity, generate and store a new one
		identity, err = xxdk.MakeReceptionIdentity(net)
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to generate reception identity: %+v", logPrefix, err)
		}
		err = xxdk.StoreReceptionIdentity(identityStorageKey, identity, net)
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to store new reception identity: %+v", logPrefix, err)
		}
	}

	// Create an E2E client
	params := xxdk.GetDefaultE2EParams()
	user, err := xxdk.Login(net, xxdk.DefaultAuthCallbacks{}, identity, params)
	if err != nil {
		jww.FATAL.Panicf("[%s] Unable to Login: %+v", logPrefix, err)
	}

	// Start a stream
	stream := user.GetRng().GetStream()

	// Get the group
	grp, err := identity.GetGroup()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get group from identity: %+v", logPrefix, err)
	}

	// Create Client
	return &Client{
		user,
		stream,
		grp,
	}
}

// ---------------------------- //
// Start the Client
// This function starts the cMix network follower
// then waits until the Client is connected to the network
func (c *Client) Start() {
	// Start cMix network follower
	networkFollowerTimeout := 5 * time.Second
	err := c.user.StartNetworkFollower(networkFollowerTimeout)
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to start cMix network follower: %+v", logPrefix, err)
	}

	// Create a tracker channel to be notified of network changes
	connected := make(chan bool, 10)
	// Provide a callback that will be signalled when network
	// health status changes
	c.user.GetCmix().AddHealthCallback(
		func(isConnected bool) {
			connected <- isConnected
		})

	// Wait until connected or crash on timeout
	waitTimeout := 30 * time.Second
	timeoutTimer := time.NewTimer(waitTimeout)
	isConnected := false
	for !isConnected {
		select {
		case isConnected = <-connected:
		case <-timeoutTimer.C:
			jww.FATAL.Panicf("[%s] Timeout on starting cMix Client", logPrefix)
		}
	}
	jww.INFO.Printf("[%s] Started cMix Client", logPrefix)
}

// ---------------------------- //
// Stop the Client
func (c *Client) Stop() {
	// Stop cMix network follower
	err := c.user.StopNetworkFollower()
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to stop cMix network follower: %+v", logPrefix, err)
	} else {
		jww.INFO.Printf("[%s] Stopped cMix network follower", logPrefix)
	}

	// Close Stream
	c.stream.Close()
	jww.INFO.Printf("[%s] Stopped cMix Client", logPrefix)
}

type Request struct {
	method  restlike.Method
	uri     string
	data    []byte
	headers []byte
}

// ---------------------------- //
// Send a single-use REST request to a given contact
func (c *Client) Request(contact contact.Contact, req Request) (*restlike.Message, error) {
	// Build request
	request := restSingle.Request{
		Net:    c.user.GetCmix(),
		Rng:    c.stream,
		E2eGrp: c.grp,
	}

	// Send request and wait for response
	jww.INFO.Printf("[%s] Sending cMix request with content: %v", logPrefix, string(req.data))
	response, err := request.Request(contact,
		req.method, restlike.URI(req.uri), req.data, &restlike.Headers{Headers: req.headers},
		single.GetDefaultRequestParams(),
	)
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to send request over cMix: %+v", logPrefix, err)
		return nil, err
	}
	return response, nil
}
