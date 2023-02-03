package cmd

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/crypto/contact"
)

type Api struct {
	client            *Client
	serverContact     contact.Contact
	networks          []string
	supportedNetworks map[string]struct{}
}

// ---------------------------- //
// Create a new API instance to
// access blockchains over cMix
// Input: the filepath of the server
// contact file
// Panics on failure to open and parse
// contact data
func NewApi(contactFile string) *Api {
	// Load server contact from file
	contactData, err := os.ReadFile(contactFile)
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to read server contact file: %+v", logPrefix, err)
	}

	// Unmarshal contact data
	serverContact, err := contact.Unmarshal(contactData)
	if err != nil {
		jww.FATAL.Panicf("[%s] Failed to get server contact data: %+v", logPrefix, err)
	}

	// Create cMix client
	client := NewClient()

	return &Api{
		client:            client,
		serverContact:     serverContact,
		networks:          nil,
		supportedNetworks: nil,
	}
}

// ---------------------------- //
// Connect the API to the REST server
// Starts cMix client
// Loads supported networks from server
// Returns an error if it can't connect
// to the server over cMix and get
// supported networks
func (a *Api) Connect() error {
	// Start cMix client
	a.client.Start()

	// Get supported networks from server
	resp, _, err := a.doRequest(restlike.Get, "/networks", nil)
	if err != nil {
		errMsg := fmt.Sprintf("Couldn't get supported networks: %v", err)
		jww.ERROR.Printf("[%s] %v", logPrefix, errMsg)
		return errors.New(errMsg)
	}
	err = json.Unmarshal(resp, &a.networks)
	if err != nil {
		errMsg := fmt.Sprintf("Couldn't get supported networks: %v", err)
		jww.ERROR.Printf("[%s] %v", logPrefix, errMsg)
		return errors.New(errMsg)
	}

	// Build map of supported networks for fast lookup
	a.supportedNetworks = make(map[string]struct{})
	for _, n := range a.networks {
		a.supportedNetworks[n] = struct{}{}
	}
	return nil
}

// ---------------------------- //
// Disconnect the API
// Stops cMix client
// Clears supported networks
func (a *Api) Disconnect() {
	// Stop cMix Client
	a.client.Stop()

	// Clear supported networks
	a.networks = nil
	for k := range a.supportedNetworks {
		delete(a.supportedNetworks, k)
	}
	a.supportedNetworks = nil
}

// ---------------------------- //
// Return list of supported networks
// NOTE: this list is loaded from the server
// on Api.Connect()
func (a *Api) Networks() []string {
	return a.networks
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

// do a request over cMix
func (a *Api) doRequest(
	method restlike.Method,
	uri string,
	data []byte,
) ([]byte, int, error) {
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

	// Make sure the network is supported
	// (except for when getting supported networks)
	if _, ok := a.supportedNetworks[uri]; !ok && uri != "/networks" {
		jww.ERROR.Printf("[%s] Network %v is not supported", logPrefix, uri)
		return nil, 400, errors.New("unsupported network")
	}

	// Build request
	request := Request{
		method:  method,
		uri:     uri,
		data:    data,
		headers: headers,
	}

	// Do request over cMix
	// Repeat for number of retries
	tries := 1
	response, err := a.client.Request(a.serverContact, request)
	for err != nil {
		response, err = a.client.Request(a.serverContact, request)
		tries++
		if tries > retries {
			break
		}
	}

	// Bail if can't do request in specified number of retries
	if err != nil {
		jww.ERROR.Printf("[%s] Failed to send request after %v retries, bailing", logPrefix, retries)
		return nil, 500, errors.New("request exhausted number of retries")
	}

	// Parse code from headers
	code := 500
	if response.Headers != nil && len(response.Headers.Headers) >= 2 {
		code = int(binary.LittleEndian.Uint16(response.Headers.Headers))
	}

	// Parse response error
	if response.Error != "" {
		errMsg := fmt.Sprintf("Response error: %v", response.Error)
		jww.ERROR.Printf("[%s] %v", logPrefix, errMsg)
		return nil, code, errors.New(errMsg)
	} else {
		return response.Content, code, nil
	}
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
