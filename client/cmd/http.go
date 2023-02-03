package cmd

import (
	"fmt"
	"io"
	"net/http"

	jww "github.com/spf13/jwalterweatherman"
)

type HttpProxy struct {
	api *Api
}

func NewHttpProxy(api *Api) *HttpProxy {
	return &HttpProxy{api}
}

// Start the HTTP proxy server
// This function blocks on listening for connections
// Panics on error different than server closed
func (hp *HttpProxy) Start() {
	jww.INFO.Printf("[%s] Starting HTTP server on port: %v", logPrefix, port)
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%v", port), hp); err != http.ErrServerClosed {
		jww.FATAL.Panicf("[%s] Error starting HTTP server", logPrefix)
	}
}

func (hp *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			jww.ERROR.Printf("[%s] Body reading error: %v", logPrefix, err)
			// 500 Internal Server Error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()
		if len(data) > 0 {
			jww.INFO.Printf("[%s] Got HTTP request: %v", logPrefix, string(data))
			resp, code, err := hp.api.Request(r.RequestURI, data)
			if err != nil {
				jww.ERROR.Printf("[%s] Request returned an error: %v", logPrefix, err)
				// 500 Internal Server Error
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				// Code from server
				// Can be 200 OK, 400 Bad Request or 500 Internal Server Error
				w.WriteHeader(code)
				if _, err := w.Write(resp); err != nil {
					jww.ERROR.Printf("[%s] Error writing to HTTP connection: %v", logPrefix, err)
				} else {
					jww.INFO.Printf("[%s] Response: %v", logPrefix, string(resp))
				}
			}
		} else {
			jww.WARN.Printf("[%s] Empty body request", logPrefix)
			// 400 Bad Request
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}
