package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/client/v4/connect"
	"gitlab.com/elixxir/client/v4/e2e/receive"
	"gitlab.com/elixxir/client/v4/xxdk"
)

// ---------------------------- //
// Connect Server
type ConnectServer struct {
	// Active proxy connections
	connections map[uint32]*Proxy
	num         uint32
	mux         sync.Mutex
}

func NewConnectServer() *ConnectServer {
	return &ConnectServer{
		connections: make(map[uint32]*Proxy),
		num:         0,
	}
}

// ---------------------------- //
// This is the callback function called by xxDK in order
// to process an incoming connection
func (c *ConnectServer) Connect(connection connect.Connection) {
	sender := connection.GetPartner().PartnerId()
	jww.INFO.Printf("[%s] Connection received over cMix from %s", logPrefix, sender)
	c.mux.Lock()
	defer c.mux.Unlock()
	p := NewProxy(connection, c.num)
	c.connections[c.num] = p
	c.num++
	_, err := connection.RegisterListener(MsgType, p)
	if err != nil {
		jww.ERROR.Printf("[%s] Error registering listener: %v", logPrefix, err)
	}
}

type Proxy struct {
	cmixConn connect.Connection
	num      uint32
	// Active connections
	conns map[uint32]*Conn
	mux   sync.RWMutex
}

func NewProxy(connection connect.Connection, num uint32) *Proxy {
	return &Proxy{
		cmixConn: connection,
		num:      num,
		conns:    make(map[uint32]*Conn),
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
	if msg.Command == "connect" {
		// Connect to remote server
		// Get URI from message
		uri := string(msg.Data)
		jww.INFO.Printf("[%s] Connecting to (id-%d): %s", logPrefix, msg.ID, uri)
		// Create connection
		conn := NewConn(msg.ID, uri, p)
		// Start connection
		// This dials the TCP connection, and starts the read routine
		err := conn.Start()
		if err != nil {
			jww.ERROR.Printf("[%s] Error connecting to %s: %v", logPrefix, uri, err)
			return
		}
		// Add connection to map
		p.mux.Lock()
		if _, ok := p.conns[msg.ID]; ok {
			jww.WARN.Printf("[%s] Connection (id-%d) already exists, replacing", logPrefix, msg.ID)
			p.conns[msg.ID].Stop()
		}
		p.conns[msg.ID] = conn
		p.mux.Unlock()
		// Send ACK back to client
		// Build message to send to client
		message := &Message{
			Command: "ack",
			ID:      msg.ID,
			Data:    nil,
		}
		// Send message over cMix
		err = conn.sendMessage(message)

		if err != nil {
			jww.ERROR.Printf("[%s] Error sending ack message to client: %v", logPrefix, err)
			conn.Stop()
			p.removeConn(conn.id)
		}
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
	return fmt.Sprintf("Proxy-%d", p.num)
}

func (p *Proxy) removeConn(id uint32) {
	p.mux.Lock()
	delete(p.conns, id)
	p.mux.Unlock()
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

func NewConn(id uint32, uri string, p *Proxy) *Conn {
	e2eParams := xxdk.GetDefaultE2EParams()
	return &Conn{
		id:           id,
		uri:          uri,
		p:            p,
		params:       e2eParams,
		tcpConn:      nil,
		stopped:      false,
		writeCounter: 0,
		readCounter:  0,
		bufferReads:  make(map[uint32]Message, 10),
	}
}

func (c *Conn) Start() error {
	conn, err := net.Dial("tcp", c.uri)
	if err != nil {
		jww.ERROR.Printf("[%s] Error connecting to %s: %v", logPrefix, c.uri, err)
		return err
	}
	c.tcpConn = conn
	go c.process()
	go c.read()
	return nil
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
	// to the client. But only when the connection was not closed by calling Stop()
	if !c.stopped {
		// Quit the process routine
		c.stopped = true
		// Build message to send to client
		message := &Message{
			Command: "close",
			ID:      c.id,
			Data:    nil,
			Counter: c.writeCounter,
		}
		// Send message over cMix
		err := c.sendMessage(message)
		if err != nil {
			jww.ERROR.Printf("[%s] Error sending close message to client: %v", logPrefix, err)
		}
	}
}

func (c *Conn) Write(p []byte) (n int, err error) {
	// Build message to send to client
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
