package cmd

import (
	"os"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/client/v4/restlike/single"
	"gitlab.com/elixxir/client/v4/xxdk"
	"gitlab.com/xx_network/primitives/utils"
)

// ---------------------------- //
// Server holds the REST Server and xxDK user
type Server struct {
	restServer *single.Server
	user       *xxdk.E2e
}

// ---------------------------- //
// Constructors

// Initialize a new cMix Server
// This function initializes the state from the configured path
// and writes the contact information to the provided filepath
func InitializeServer() {
	// Create Server
	newServer(true)
}

// Load a cMix RestLike Server
// The function attempts to load server state from the configured path
// It panics if the state directory doesn't exist
func LoadServer() *Server {
	// Create Server
	net, identity := newServer(false)

	// Create an E2E client
	params := xxdk.GetDefaultE2EParams()
	user, err := xxdk.Login(net, xxdk.DefaultAuthCallbacks{}, identity, params)
	if err != nil {
		jww.FATAL.Panicf("[%s] Unable to Login: %+v", logPrefix, err)
	}

	// Pull the reception identity information
	dhKeyPrivateKey, err := identity.GetDHKeyPrivate()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get DH private key from identity: %+v", logPrefix, err)
	}

	// Get the group
	grp, err := identity.GetGroup()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get group from identity: %+v", logPrefix, err)
	}

	// Initialize the server
	restServer := single.NewServer(identity.ID, dhKeyPrivateKey, grp, user.GetCmix())
	jww.INFO.Printf("[%s] Initialized single use REST Server", logPrefix)

	return &Server{
		restServer,
		user,
	}
}

// ---------------------------- //
// Get REST Server endpoints
func (s *Server) GetEndpoints() *restlike.Endpoints {
	return s.restServer.GetEndpoints()
}

// ---------------------------- //
// Start the REST Server
// This function starts the cMix network follower
// then waits until the Server is connected to the network
func (s *Server) Start() {
	// Start cMix network follower
	networkFollowerTimeout := 5 * time.Second
	err := s.user.StartNetworkFollower(networkFollowerTimeout)
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to start cMix network follower: %+v", logPrefix, err)
	}

	// Create a tracker channel to be notified of network changes
	connected := make(chan bool, 10)
	// Provide a callback that will be signalled when network
	// health status changes
	s.user.GetCmix().AddHealthCallback(
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
			jww.FATAL.Panicf("[%s] Timeout on starting REST Server", logPrefix)
		}
	}
	jww.INFO.Printf("[%s] Started REST Server", logPrefix)
}

// ---------------------------- //
// Stop the REST Server
func (s *Server) Stop() {
	// Stop cMix network follower
	err := s.user.StopNetworkFollower()
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to stop cMix network follower: %+v", logPrefix, err)
	} else {
		jww.INFO.Printf("[%s] Stopped cMix network follower", logPrefix)
	}

	// Close REST server
	s.restServer.Close()
	jww.INFO.Printf("[%s] Stopped REST Server", logPrefix)
}

// ---------------------------- //
// Internal functions
// ---------------------------- //

func newServer(initialize bool) (*xxdk.Cmix, xxdk.ReceptionIdentity) {
	// Initialize state if requested
	// Overwrites existing state if found at provided path
	_, err := os.Stat(statePath)
	if initialize {
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
		if initialize {
			// If no extant xxdk.ReceptionIdentity, generate and store a new one
			identity, err = xxdk.MakeReceptionIdentity(net)
			if err != nil {
				jww.FATAL.Panicf("[%s] Failed to generate reception identity: %+v", logPrefix, err)
			}
			err = xxdk.StoreReceptionIdentity(identityStorageKey, identity, net)
			if err != nil {
				jww.FATAL.Panicf("[%s] Failed to store new reception identity: %+v", logPrefix, err)
			}
		} else {
			jww.FATAL.Panicf("[%s] Failed to load reception identity: %+v", logPrefix, err)
		}
	}

	// Save the server contact file at the provided filepath
	if initialize {
		err = utils.WriteFileDef(outputFile, identity.GetContact().Marshal())
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed writing contact file to %v: %+v", logPrefix, outputFile, err)
		}
	}

	return net, identity
}
