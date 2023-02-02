package cmd

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
	"net/url"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/restlike"
)

// Execute the query to one of the endpoints, randomly selected
func doQuery(endpoints []string, data []byte) ([]byte, error) {
	// Get endpoint
	endpoint := endpoints[0]
	if len(endpoints) > 1 {
		endpoint = endpoints[rand.Intn(len(endpoints))]
	}

	// Query
	body, _, err := queryJsonRpc(endpoint, data)
	return body, err
}

var testRequest = "{\"id\":\"1\", \"jsonrpc\":\"2.0\", \"method\": \"\", \"params\":[]}"

// Test connection to an endpoint supporting JSON-RPC format
func testConnectJsonRpc(url string) bool {
	valid := true
	_, code, err := queryJsonRpc(url, []byte(testRequest))
	if err != nil {
		valid = false
	} else {
		if code != 200 && code != 400 {
			jww.INFO.Printf("[RELAY] Endpoint %v returned code %v", url, code)
			valid = false
		}
	}
	return valid
}

// Perform HTTP POST request with JSON-RPC format
func queryJsonRpc(url string, data []byte) ([]byte, int, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		jww.ERROR.Printf("[RELAY] Error creating request to query %v: %v", url, err)
		return nil, 500, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		jww.ERROR.Printf("[RELAY] Error performing request to %v: %v", url, err)
		return nil, 500, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

// Extract the RPC endpoint from the request headers
func getEndpointFromHeaders(headers *restlike.Headers) string {
	// 1. Check if headers are empty
	if headers == nil || len(headers.Headers) == 0 {
		jww.INFO.Print("[RELAY] Empty headers in custom URI request")
		return ""
	}

	// 2. Get and validate URL from headers
	url := string(headers.Headers)
	if isValidHTTPSURL(url) {
		return url
	} else {
		return ""
	}
}

// Validate an HTTPS URL
func isValidHTTPSURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		jww.INFO.Print("[RELAY] Couldn't parse URL from headers")
		return false
	}
	if u.Scheme != "https" {
		jww.INFO.Print("[RELAY] URL is not HTTPS")
		return false
	}
	return true
}
