package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"github.com/xx-labs/blockchain-cmix-relay/cmix"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/crypto/contact"
)

type HttpProxy struct {
	c         *cmix.Client
	port      int
	contact   contact.Contact
	logPrefix string
	srv       *http.Server
}

func NewHttpProxy(c *cmix.Client, port int, contactFile, logPrefix string) *HttpProxy {
	contact := cmix.LoadContactFile(contactFile)
	hp := &HttpProxy{c, port, contact, logPrefix, nil}
	hp.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: hp,
	}
	return hp
}

// Start the HTTP proxy server
// This function blocks on listening for connections
// Panics on error different than server closed
func (hp *HttpProxy) Start() {
	jww.INFO.Printf("[%s] Starting HTTP server on port: %v", hp.logPrefix, hp.port)
	if err := hp.srv.ListenAndServe(); err != http.ErrServerClosed {
		jww.FATAL.Panicf("[%s] Error starting HTTP server", hp.logPrefix)
	}
}

// Stop the Http server
func (hp *HttpProxy) Stop() {
	jww.INFO.Printf("[%s] Stopping HTTP server on port: %v", hp.logPrefix, hp.port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer func() {
		cancel()
	}()
	if err := hp.srv.Shutdown(ctx); err != nil {
		jww.FATAL.Panicf("[%s] Error stopping HTTP server: %v", hp.logPrefix, err)
	}
	jww.INFO.Printf("[%s] HTTP stopped", hp.logPrefix)
}

type Header struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// Handle requests
func (hp *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var data []byte
	var err error
	if r.Body != nil {
		data, err = io.ReadAll(r.Body)
		if err != nil {
			jww.ERROR.Printf("[%s] Body reading error: %v", hp.logPrefix, err)
			// 500 Internal Server Error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()
	}
	// Copy headers to internal representation
	headers := make([]Header, 0, len(r.Header)+2)
	for k, v := range r.Header {
		headers = append(headers, Header{k, v})
	}
	// Put the URL and Method in headers
	// Build URL based on Host
	url := ""
	// If URI starts with /, this is probably a localhost request so need to prepend Host
	if r.RequestURI[0] == '/' {
		url = r.Host + r.RequestURI
	} else {
		// Otherwise this is probably a a proxied request, so use the URI directly
		url = r.RequestURI
	}

	headers = append(headers, Header{"X-PROXXY-URL", []string{url}})
	headers = append(headers, Header{"X-PROXXY-METHOD", []string{r.Method}})
	// Copy headers to cmix request
	headerData, err := json.Marshal(headers)
	if err != nil {
		jww.ERROR.Printf("[%s] Error marshalling Headers: %v", hp.logPrefix, err)
		// 500 Internal Server Error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	////////////////////////////////
	// REQUEST
	req := cmix.Request{
		Method:  restlike.Get,
		Uri:     "/proxy",
		Data:    data,
		Headers: headerData,
	}
	resp, err := hp.c.Request("http-proxy", hp.contact, req)
	if err != nil {
		jww.ERROR.Printf("[%s] Request error: %v", hp.logPrefix, err)
		// 500 Internal Server Error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	////////////////////////////////
	// RESPONSE
	// No headers means server error
	if len(resp.Headers.Headers) == 0 {
		jww.ERROR.Printf("[%s] No headers in response, server error", hp.logPrefix)
		// 500 Internal Server Error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Parse headers from response
	var respHeaders []Header
	err = json.Unmarshal(resp.Headers.Headers, &respHeaders)
	if err != nil {
		jww.ERROR.Printf("[%s] Error unmarshalling Headers: %v", hp.logPrefix, err)
		// 500 Internal Server Error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	// Convert headers to HTTP headers
	httpHeaders := make(http.Header, len(respHeaders))
	for _, header := range respHeaders {
		for _, val := range header.Values {
			httpHeaders.Add(header.Key, val)
		}
	}
	// Get code from headers and delete
	code := httpHeaders.Get("X-PROXXY-RESPCODE")
	httpHeaders.Del("X-PROXXY-RESPCODE")

	// Write headers
	for k, v := range httpHeaders {
		w.Header()[k] = v
	}

	// Write code
	codeInt, _ := strconv.Atoi(code)
	w.WriteHeader(codeInt)

	// Write content if set
	if resp.Content != nil {
		if _, err := w.Write(resp.Content); err != nil {
			jww.ERROR.Printf("[%s] Error writing to HTTP connection: %v", hp.logPrefix, err)
		} else {
			jww.INFO.Printf("[%s] Got response", hp.logPrefix)
		}
	}
}
