package api

import (
	"os"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/crypto/contact"
)

func LoadContactFile(file string) contact.Contact {
	// Load server contact from file
	contactData, err := os.ReadFile(file)
	if err != nil {
		jww.FATAL.Panicf("Failed to read server contact file: %+v", err)
	}

	// Unmarshal contact data
	serverContact, err := contact.Unmarshal(contactData)
	if err != nil {
		jww.FATAL.Panicf("Failed to get server contact data: %+v", err)
	}

	return serverContact
}
