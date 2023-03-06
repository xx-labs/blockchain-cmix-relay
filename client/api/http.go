package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	jww "github.com/spf13/jwalterweatherman"
)

type HttpProxy struct {
	api       *Api
	port      int
	logPrefix string
	srv       *http.Server
}

func NewHttpProxy(api *Api, port int, logPrefix string) *HttpProxy {
	hp := &HttpProxy{api, port, logPrefix, nil}
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

// Handle requests
func (hp *HttpProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			jww.ERROR.Printf("[%s] Body reading error: %v", hp.logPrefix, err)
			// 500 Internal Server Error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()
		if len(data) > 0 {
			jww.INFO.Printf("[%s] Got HTTP request: %v", hp.logPrefix, string(data))
			resp, code, err := hp.api.Request(r.RequestURI, data)
			if err != nil {
				jww.ERROR.Printf("[%s] Request returned an error: %v", hp.logPrefix, err)
				// 500 Internal Server Error
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				// Code from server
				// Can be 200 OK, 400 Bad Request or 500 Internal Server Error
				w.WriteHeader(code)
				if _, err := w.Write(resp); err != nil {
					jww.ERROR.Printf("[%s] Error writing to HTTP connection: %v", hp.logPrefix, err)
				} else {
					jww.INFO.Printf("[%s] Response: %v", hp.logPrefix, string(resp))
				}
			}
		} else {
			jww.WARN.Printf("[%s] Empty body request", hp.logPrefix)
			// 400 Bad Request
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}
