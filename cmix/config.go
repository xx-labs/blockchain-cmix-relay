package cmix

import (
	"os"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/crypto/contact"
)

// Configuration for cMix client/server
type Config struct {
	// Logging
	LogPrefix string

	// xxDK API
	Cert          string
	NdfUrl        string
	StatePath     string
	StatePassword string
}

func LoadContactFile(file string) contact.Contact {
	// Load server contact from file
	contactData, err := os.ReadFile(file)
	if err != nil {
		jww.FATAL.Panicf("Failed to read server contact file: %+v", err)
	}
	return UnmarshalContact(contactData)
}

func UnmarshalContact(data []byte) contact.Contact {
	// Unmarshal contact data
	serverContact, err := contact.Unmarshal(data)
	if err != nil {
		jww.FATAL.Panicf("Failed to get server contact data: %+v", err)
	}

	return serverContact
}
