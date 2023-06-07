package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
)

// ---------------------------- //
// HTTP Proxy
type HttpProxy struct{}

type Header struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// ---------------------------- //
// This is the callback function called by xxDK in order
// to process a restlike request
// This function proxies an HTTP request received over cMix
func (h *HttpProxy) Callback(request *restlike.Message) *restlike.Message {
	jww.INFO.Printf("[%s] Request received over cMix: %v", logPrefix, request)

	// Response
	respHeaders := make([]Header, 0)
	response := &restlike.Message{}
	response.Headers = &restlike.Headers{}
	response.Content = nil

	// Start with code 400 (Bad Request)
	code := "400"

	// Parse headers from request
	var headers []Header
	err := json.Unmarshal(request.Headers.Headers, &headers)
	if err != nil {
		jww.ERROR.Printf("[%s] Error parsing request headers: %v", logPrefix, err)
	} else {
		// Convert headers to HTTP headers
		httpHeaders := make(http.Header, len(headers))
		for _, header := range headers {
			for _, val := range header.Values {
				httpHeaders.Add(header.Key, val)
			}
		}

		// Print headers
		for k, v := range httpHeaders {
			jww.INFO.Printf("[%s] Header: %v: %v", logPrefix, k, v)
		}

		// Get URL and Method from headers
		url := httpHeaders.Get("X-PROXXY-URL")
		method := httpHeaders.Get("X-PROXXY-METHOD")

		// Create HTTP request
		req, err := http.NewRequest(method, url, bytes.NewBuffer(request.Content))
		if err != nil {
			jww.ERROR.Printf("[%s] Error creating %s HTTP request to %v: %v", logPrefix, method, url, err)
			code = "500"
		} else {
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				jww.ERROR.Printf("[%s] Error performing %s HTTP request to %v: %v", logPrefix, method, url, err)
				code = "500"
			} else {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				// Copy headers from HTTP response
				for k, v := range resp.Header {
					respHeaders = append(respHeaders, Header{k, v})
				}
				// Copy code from HTTP response
				code = fmt.Sprintf("%d", resp.StatusCode)
				// Copy body from HTTP response
				response.Content = body
			}
		}
	}
	// Set code in headers
	respHeaders = append(respHeaders, Header{"X-PROXXY-RESPCODE", []string{code}})
	// Copy headers to cmix response
	headerData, err := json.Marshal(respHeaders)
	if err != nil {
		jww.ERROR.Printf("[%s] Error marshalling response headers: %v", logPrefix, err)
		// Client will catch this as an internal server error
	} else {
		response.Headers.Headers = headerData
	}
	return response
}
