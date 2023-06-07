package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"github.com/xx-labs/blockchain-cmix-relay/client/api"
	"gitlab.com/elixxir/client/v4/connect"
	"gitlab.com/elixxir/client/v4/e2e/receive"
	"gitlab.com/elixxir/client/v4/restlike"
	"gitlab.com/elixxir/client/v4/xxdk"
	"gitlab.com/elixxir/crypto/contact"
)

type HttpProxy struct {
	cmix      *api.Client
	port      int
	contact   contact.Contact
	logPrefix string
	srv       *http.Server

	proxy *Proxy
}

func NewHttpProxy(cmix *api.Client, port int, contactFile, contactFileConnect, logPrefix string) *HttpProxy {
	contact := api.LoadContactFile(contactFile)
	contactConnect := api.LoadContactFile(contactFileConnect)
	jww.INFO.Printf("[%s] Attempting to connect to relayer over CMIX", logPrefix)
	handler, err := connect.Connect(contactConnect, cmix.User(), xxdk.GetDefaultE2EParams())
	if err != nil {
		jww.FATAL.Panicf("Failed to create connection object: %+v", err)
	}
	p := NewProxy(handler, logPrefix)
	_, err = handler.RegisterListener(MsgType, p)
	if err != nil {
		jww.FATAL.Panicf("Failed to create connection object: %+v", err)
	}
	hp := &HttpProxy{cmix, port, contact, logPrefix, nil, p}
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
	if r.Method == "CONNECT" {
		hp.proxy.handleConnect(w, r)
	} else {
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
		headers = append(headers, Header{"X-PROXXY-URL", []string{r.RequestURI}})
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
		req := api.Request{
			Method:  restlike.Get,
			Uri:     "/proxy",
			Data:    data,
			Headers: headerData,
		}
		resp, err := hp.cmix.Request("http-proxy", hp.contact, req)
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
				jww.INFO.Printf("[%s] Response: %v", hp.logPrefix, string(resp.Content))
			}
		}
	}
}

type Proxy struct {
	cmixConn connect.Connection
	num      uint32
	// Active connections
	conns     map[uint32]*Conn
	mux       sync.RWMutex
	logPrefix string
}

func NewProxy(connection connect.Connection, logPrefix string) *Proxy {
	return &Proxy{
		cmixConn:  connection,
		num:       0,
		conns:     make(map[uint32]*Conn),
		logPrefix: logPrefix,
	}
}

const MsgType = 3

// Message format
// Command is one of
//   - "connect"
//   - "ack"
//   - "data"
//   - "close"
type Message struct {
	Command string `json:"command"`
	ID      uint32 `json:"id"`
	Data    []byte `json:"data"`
	Counter uint32 `json:"counter"`
}

// Hear will be called whenever a message matching the
// RegisterListener call is received.
func (p *Proxy) Hear(item receive.Message) {
	jww.INFO.Printf("[%s] Message received over cMix from: %s", logPrefix, item.Sender)
	// Unmarshal message
	var msg Message
	err := json.Unmarshal(item.Payload, &msg)
	if err != nil {
		jww.ERROR.Printf("[%s] Error parsing message: %v", logPrefix, err)
		return
	}
	if msg.Command == "ack" {
		jww.INFO.Printf("[%s] Accepting connection (id-%d)", logPrefix, msg.ID)
		p.mux.RLock()
		if _, ok := p.conns[msg.ID]; ok {
			// Accept connection
			p.conns[msg.ID].Start()
			p.conns[msg.ID].tcpConn.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n"))
		} else {
			jww.WARN.Printf("[%s] Connection (id-%d) does not exist", logPrefix, msg.ID)
		}
		p.mux.RUnlock()
	} else {
		p.mux.RLock()
		if _, ok := p.conns[msg.ID]; ok {
			go p.conns[msg.ID].Receive(msg)
		} else {
			jww.WARN.Printf("[%s] Connection (id-%d) does not exist", logPrefix, msg.ID)
		}
		p.mux.RUnlock()
	}
}

// Name is used for debugging purposes.
func (p *Proxy) Name() string {
	return "HTTP-Proxy"
}

func (p *Proxy) removeConn(id uint32) {
	p.mux.Lock()
	delete(p.conns, id)
	p.mux.Unlock()
}

// Handle CONNECT requests
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hij, ok := w.(http.Hijacker)
	if !ok {
		panic("httpserver does not support hijacking")
	}

	tcpConn, _, e := hij.Hijack()
	if e != nil {
		panic("Cannot hijack connection " + e.Error())
	}

	// Create connection
	uri := r.Host
	p.mux.Lock()
	conn := NewConn(p.num, uri, p, tcpConn)
	p.conns[p.num] = conn
	p.num++
	p.mux.Unlock()

	// Send connect message to server
	// Build message to send to server
	message := &Message{
		Command: "connect",
		ID:      conn.id,
		Data:    []byte(uri),
	}

	// Send message over cMix
	err := conn.sendMessage(message)

	if err != nil {
		jww.ERROR.Printf("[%s] Error sending connect message to server: %v", logPrefix, err)
		p.removeConn(conn.id)
		// 500 Internal Server Error
		conn.Write([]byte("HTTP/1.0 500 Internal Server Error\r\n\r\n"))
	}

	// Server will reply with ACK message, which is handled by the Hear function
}

type Conn struct {
	id      uint32
	uri     string
	p       *Proxy
	params  xxdk.E2EParams
	tcpConn net.Conn
	stopped bool

	// Data ordering
	writeCounter uint32
	readCounter  uint32
	bufferReads  map[uint32]Message
	mux          sync.Mutex
}

func NewConn(id uint32, uri string, p *Proxy, conn net.Conn) *Conn {
	e2eParams := xxdk.GetDefaultE2EParams()
	return &Conn{
		id:           id,
		uri:          uri,
		p:            p,
		params:       e2eParams,
		tcpConn:      conn,
		stopped:      false,
		writeCounter: 0,
		readCounter:  0,
		bufferReads:  make(map[uint32]Message, 10),
	}
}

func (c *Conn) Start() {
	go c.process()
	go c.read()
}

func (c *Conn) Stop() {
	c.stopped = true
	c.tcpConn.Close()
}

func (c *Conn) Receive(msg Message) {
	c.mux.Lock()
	c.bufferReads[msg.Counter] = msg
	c.mux.Unlock()
}

func (c *Conn) process() {
	ticker := time.NewTicker(50 * time.Millisecond)
	for range ticker.C {
		// Check if stopped and quit
		if c.stopped {
			return
		}
		// Check buffer
		c.mux.Lock()
		if msg, ok := c.bufferReads[c.readCounter]; ok {
			// Process message
			switch msg.Command {
			case "data":
				// Send data to client
				jww.INFO.Printf("[%s] Sending data to connection (id-%d)", logPrefix, msg.ID)
				c.tcpConn.Write(msg.Data)
			case "close":
				// Close connection
				jww.INFO.Printf("[%s] Closing connection (id-%d)", logPrefix, msg.ID)
				c.Stop()
				c.p.removeConn(c.id)
			}
			// Delete from buffer
			delete(c.bufferReads, c.readCounter)
			// Increment counter
			c.readCounter++
		}
		c.mux.Unlock()
	}
}

func (c *Conn) read() {
	if _, err := io.Copy(c, c.tcpConn); err != nil {
		jww.ERROR.Printf("[%s] Error reading from %s: %v", logPrefix, c.uri, err)
	}

	// When the TCP connection closes, we should send a close message
	// to the server. But only when the connection was not closed by calling Stop()
	if !c.stopped {
		// Quit the process routine
		c.stopped = true
		// Build message to send to server
		message := &Message{
			Command: "close",
			ID:      c.id,
			Data:    nil,
			Counter: c.writeCounter,
		}
		// Send message over cMix
		err := c.sendMessage(message)
		if err != nil {
			jww.ERROR.Printf("[%s] Error sending close message to server: %v", logPrefix, err)
		}
	}
}

func (c *Conn) Write(p []byte) (n int, err error) {
	// Build message to send to server
	message := &Message{
		Command: "data",
		ID:      c.id,
		Data:    p,
		Counter: c.writeCounter,
	}
	c.writeCounter++
	// Send message over cMix
	err = c.sendMessage(message)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) sendMessage(msg *Message) error {
	// Marshal message
	data, err := json.Marshal(msg)
	if err != nil {
		jww.ERROR.Printf("[%s] Error marshaling message: %v", logPrefix, err)
		return err
	}
	// Send message over cMix
	sendReport, err := c.p.cmixConn.SendE2E(MsgType, data, c.params.Base)
	if err != nil {
		jww.ERROR.Printf("[%s] Error sending message over cMix: %v", logPrefix, err)
		return err
	}
	// Print send report
	jww.INFO.Printf("[%s] %s Message %s sent in RoundIDs: %+v",
		logPrefix, msg.Command, sendReport.MessageId, sendReport.RoundList)
	return nil
}
