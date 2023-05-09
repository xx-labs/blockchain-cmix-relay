package api

import (
	"math/rand"
	"os"
	"strings"
	"time"

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
