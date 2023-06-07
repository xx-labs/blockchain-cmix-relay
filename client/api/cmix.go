package api

import (
	"errors"
	"io/fs"
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
	user      *xxdk.E2e
	stream    *fastRNG.Stream
	grp       *cyclic.Group
	logPrefix string
}

// ---------------------------- //
// Create a new cMix client
func NewClient(c Config) *Client {
	// Initialize xxDK state
	// If state already exists, re-use it
	if _, err := os.Stat(c.StatePath); errors.Is(err, fs.ErrNotExist) {
		jww.INFO.Printf("[%s] Initializing state at %v", c.LogPrefix, c.StatePath)
		// Retrieve NDF
		cert, err := os.ReadFile(c.Cert)
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to read certificate: %v", c.LogPrefix, err)
		}

		ndfJSON, err := xxdk.DownloadAndVerifySignedNdfWithUrl(c.NdfUrl, string(cert))
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to download NDF: %+v", c.LogPrefix, err)
		}

		// Initialize the state using the state file
		err = xxdk.NewCmix(string(ndfJSON), c.StatePath, []byte(c.StatePassword), "")
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to initialize state: %+v", c.LogPrefix, err)
		}
	}

	// Load cMix
	jww.INFO.Printf("[%s] Loading state at %v", c.LogPrefix, c.StatePath)
	net, err := xxdk.LoadCmix(c.StatePath, []byte(c.StatePassword),
		xxdk.GetDefaultCMixParams())
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to load state: %+v", c.LogPrefix, err)
	}

	// Get reception identity (automatically created if one does not exist)
	identityStorageKey := "identityStorageKey"
	identity, err := xxdk.LoadReceptionIdentity(identityStorageKey, net)
	if err != nil {
		// If no extant xxdk.ReceptionIdentity, generate and store a new one
		identity, err = xxdk.MakeReceptionIdentity(net)
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to generate reception identity: %+v", c.LogPrefix, err)
		}
		err = xxdk.StoreReceptionIdentity(identityStorageKey, identity, net)
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed to store new reception identity: %+v", c.LogPrefix, err)
		}
	}

	// Create an E2E client
	params := xxdk.GetDefaultE2EParams()
	user, err := xxdk.Login(net, xxdk.DefaultAuthCallbacks{}, identity, params)
	if err != nil {
		jww.FATAL.Panicf("[%s] Unable to Login: %+v", c.LogPrefix, err)
	}

	// Start a stream
	stream := user.GetRng().GetStream()

	// Get the group
	grp, err := identity.GetGroup()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get group from identity: %+v", c.LogPrefix, err)
	}

	// Create Client
	return &Client{
		user:      user,
		stream:    stream,
		grp:       grp,
		logPrefix: c.LogPrefix,
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
		jww.FATAL.Panicf("[%s] Failed to start cMix network follower: %+v", c.logPrefix, err)
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
			jww.FATAL.Panicf("[%s] Timeout on starting cMix Client", c.logPrefix)
		}
	}
	jww.INFO.Printf("[%s] Started cMix Client", c.logPrefix)
}

// ---------------------------- //
// Stop the Client
func (c *Client) Stop() {
	// Stop cMix network follower
	err := c.user.StopNetworkFollower()
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to stop cMix network follower: %+v", c.logPrefix, err)
	} else {
		jww.INFO.Printf("[%s] Stopped cMix network follower", c.logPrefix)
	}

	// Close Stream
	c.stream.Close()
	jww.INFO.Printf("[%s] Stopped cMix Client", c.logPrefix)
}

type Request struct {
	Method  restlike.Method
	Uri     string
	Data    []byte
	Headers []byte
}

// ---------------------------- //
// Send a single-use REST request to a given contact
func (c *Client) Request(name string, contact contact.Contact, req Request) (*restlike.Message, error) {
	// Build request
	request := restSingle.Request{
		Net:    c.user.GetCmix(),
		Rng:    c.stream,
		E2eGrp: c.grp,
	}

	// Send request and wait for response
	jww.INFO.Printf("[%s] Sending request over cMix to %s", c.logPrefix, name)
	response, err := request.Request(contact,
		req.Method, restlike.URI(req.Uri), req.Data, &restlike.Headers{Headers: req.Headers},
		single.GetDefaultRequestParams(),
	)
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to send request over cMix: %+v", c.logPrefix, err)
		return nil, err
	}
	return response, nil
}

func (c *Client) User() *xxdk.E2e {
	return c.user
}
