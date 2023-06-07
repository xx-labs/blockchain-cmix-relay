package cmix

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
	logPrefix  string
}

// ---------------------------- //
// Constructors

// Initialize a new cMix Server
// This function initializes the state from the configured path
// and writes the contact information to the provided filepath
func InitializeServer(c Config, outputFile string) {
	// Create Server
	newServer(c, outputFile)
}

// Load a cMix RestLike Server
// The function attempts to load server state from the configured path
// It panics if the state directory doesn't exist
func LoadServer(c Config) *Server {
	// Create Server
	net, identity := newServer(c, "")

	// Create an E2E client
	params := xxdk.GetDefaultE2EParams()
	user, err := xxdk.Login(net, xxdk.DefaultAuthCallbacks{}, identity, params)
	if err != nil {
		jww.FATAL.Panicf("[%s] Unable to Login: %+v", c.LogPrefix, err)
	}

	// Pull the reception identity information
	dhKeyPrivateKey, err := identity.GetDHKeyPrivate()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get DH private key from identity: %+v", c.LogPrefix, err)
	}

	// Get the group
	grp, err := identity.GetGroup()
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get group from identity: %+v", c.LogPrefix, err)
	}

	// Initialize the server
	restServer := single.NewServer(identity.ID, dhKeyPrivateKey, grp, user.GetCmix())
	jww.INFO.Printf("[%s] Initialized single use REST Server", c.LogPrefix)

	return &Server{
		restServer,
		user,
		c.LogPrefix,
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
		jww.FATAL.Panicf("[%s] Failed to start cMix network follower: %+v", s.logPrefix, err)
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
			jww.FATAL.Panicf("[%s] Timeout on starting REST Server", s.logPrefix)
		}
	}
	jww.INFO.Printf("[%s] Started REST Server", s.logPrefix)
}

// ---------------------------- //
// Stop the REST Server
func (s *Server) Stop() {
	// Stop cMix network follower
	err := s.user.StopNetworkFollower()
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to stop cMix network follower: %+v", s.logPrefix, err)
	} else {
		jww.INFO.Printf("[%s] Stopped cMix network follower", s.logPrefix)
	}

	// Close REST server
	s.restServer.Close()
	jww.INFO.Printf("[%s] Stopped REST Server", s.logPrefix)
}

// ---------------------------- //
// Internal functions
// ---------------------------- //

func newServer(c Config, outputFile string) (*xxdk.Cmix, xxdk.ReceptionIdentity) {
	// Initialize state if requested
	// Overwrites existing state if found at provided path
	_, err := os.Stat(c.StatePath)
	// If an ouput file is provided, initialize the state
	initialize := outputFile != ""
	if initialize {
		if err == nil {
			jww.INFO.Printf("[%s] Removing existing state at %v", c.LogPrefix, c.StatePath)
			err = os.RemoveAll(c.StatePath)
			if err != nil {
				jww.FATAL.Panicf("[%s] Error removing existing state at %v", c.LogPrefix, c.StatePath)
			}
		}
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
	net, err := xxdk.LoadCmix(c.StatePath, []byte(c.StatePassword),
		xxdk.GetDefaultCMixParams())
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to load state: %+v", c.LogPrefix, err)
	}

	// Get reception identity (automatically created if one does not exist)
	identityStorageKey := "identityStorageKey"
	identity, err := xxdk.LoadReceptionIdentity(identityStorageKey, net)
	if err != nil {
		if initialize {
			// If no extant xxdk.ReceptionIdentity, generate and store a new one
			identity, err = xxdk.MakeReceptionIdentity(net)
			if err != nil {
				jww.FATAL.Panicf("[%s] Failed to generate reception identity: %+v", c.LogPrefix, err)
			}
			err = xxdk.StoreReceptionIdentity(identityStorageKey, identity, net)
			if err != nil {
				jww.FATAL.Panicf("[%s] Failed to store new reception identity: %+v", c.LogPrefix, err)
			}
		} else {
			jww.FATAL.Panicf("[%s] Failed to load reception identity: %+v", c.LogPrefix, err)
		}
	}

	// Save the server contact file at the provided filepath
	if initialize {
		err = utils.WriteFileDef(outputFile, identity.GetContact().Marshal())
		if err != nil {
			jww.FATAL.Panicf("[%s] Failed writing contact file to %v: %+v", c.LogPrefix, outputFile, err)
		}
	}

	return net, identity
}
