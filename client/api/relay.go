package api

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/crypto/contact"
)

// ---------------------------- //
// Relay contains information
// about a single relay server
type Relay struct {
	name      string
	client    *client
	contact   contact.Contact
	logPrefix string
	retries   int

	networks          []string
	supportedNetworks map[string]struct{}
	mux               sync.RWMutex

	stopChan chan struct{}
	cb       func(string, bool)
}

func NewRelay(name string, client *client, contact contact.Contact, logPrefix string, retries int) *Relay {
	return &Relay{
		name:      name,
		client:    client,
		contact:   contact,
		logPrefix: logPrefix,
		retries:   retries,
	}
}

func (r *Relay) Start(cb func(string, bool)) {
	r.cb = cb
	// Long running task to track relay server
	r.stopChan = make(chan struct{})
	go r.run()
}

func (r *Relay) Networks() []string {
	r.mux.RLock()
	defer r.mux.RUnlock()
	return r.networks
}

func (r *Relay) SupportsNetwork(network string) bool {
	r.mux.RLock()
	defer r.mux.RUnlock()
	_, ok := r.supportedNetworks[network]
	return ok
}

func (r *Relay) Stop() {
	// Stop the long running task
	r.stopChan <- struct{}{}
	close(r.stopChan)
}

func (r *Relay) Request(req Request) ([]byte, int, error) {
	response, err := r.client.request(r.contact, req)
	if err != nil {
		jww.ERROR.Printf("[%s] Error sending request to relay server %s: %v", r.logPrefix, r.name, err)
		return nil, 500, err
	}

	// Parse code from headers
	code := 500
	if response.Headers != nil && len(response.Headers.Headers) >= 2 {
		code = int(binary.LittleEndian.Uint16(response.Headers.Headers))
	}

	// Parse response error
	if response.Error != "" {
		errMsg := fmt.Sprintf("Response error: %v", response.Error)
		jww.ERROR.Printf("[%s] Relay server %s: %v", r.logPrefix, r.name, errMsg)
		return nil, code, errors.New(errMsg)
	} else {
		return response.Content, code, nil
	}
}

func (r *Relay) run() {
	ticker := time.NewTicker(60 * time.Second)
	r.requestNetworks()
	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.requestNetworks()
		}
	}
}

func (r *Relay) requestNetworks() {
	// Request networks
	req := Request{
		method:  restlike.Get,
		uri:     "/networks",
		data:    nil,
		headers: nil,
	}
	tries := 1
	resp, _, err := r.Request(req)
	for err != nil {
		tries++
		resp, _, err = r.Request(req)
		if tries >= r.retries {
			break
		}
	}
	// Check if stop was called and exit right away
	select {
	case <-r.stopChan:
		return
	default:
	}
	// Couldn't get response, notify callback that relay server is down
	if err != nil {
		jww.WARN.Printf("[%s] Failed to contact relay server %s after %v retries", r.logPrefix, r.name, r.retries)
		r.cb(r.name, false)
		return
	}
	// Got response, update supported networks and
	// notify callback that relay server is up
	r.mux.Lock()
	err = json.Unmarshal(resp, &r.networks)
	if err != nil {
		jww.ERROR.Printf("[%s] Couldn't get supported networks from relay server %s: %v", r.logPrefix, r.name, err)
		r.mux.Unlock()
		return
	}

	// Build map of supported networks for fast lookup
	for k := range r.supportedNetworks {
		delete(r.supportedNetworks, k)
	}
	r.supportedNetworks = nil
	r.supportedNetworks = make(map[string]struct{})
	for _, n := range r.networks {
		r.supportedNetworks[n] = struct{}{}
	}
	r.mux.Unlock()

	// Notify callback
	r.cb(r.name, true)
}
