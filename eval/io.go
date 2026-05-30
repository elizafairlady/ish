package eval

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"ish/core"
)

// Host I/O primitives — the irreducible socket and file layer. Sockets,
// listeners, and files are opaque core.Handle values; the in-language "port"
// is the process that owns one (the Erlang model). Every blocking call runs on
// the calling process's own goroutine, so it parks that process without
// affecting the scheduler.

const (
	handleTCPListener = "tcp-listener"
	handleTCPConn     = "tcp-conn"
	handleUDPSocket   = "udp-socket"
)

func argHandle(name string, args []Value, i int, kind string) (*core.Handle, error) {
	h, ok := args[i].(*core.Handle)
	if !ok {
		return nil, fmt.Errorf("%s: argument %d must be a %s handle, got %T", name, i+1, kind, args[i])
	}
	if kind != "" && h.Kind != kind {
		return nil, fmt.Errorf("%s: argument %d is a %s handle, want %s", name, i+1, h.Kind, kind)
	}
	return h, nil
}

func tcpListenFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-listen", args, 1); err != nil {
		return nil, err
	}
	port, ok := args[0].(core.Int)
	if !ok {
		return nil, fmt.Errorf("tcp-listen: port must be an int")
	}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return nil, fmt.Errorf("tcp-listen: %w", err)
	}
	return &core.Handle{Kind: handleTCPListener, Resource: ln}, nil
}

func tcpListenerPortFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-listener-port", args, 1); err != nil {
		return nil, err
	}
	h, err := argHandle("tcp-listener-port", args, 0, handleTCPListener)
	if err != nil {
		return nil, err
	}
	addr, ok := h.Resource.(net.Listener).Addr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("tcp-listener-port: not a TCP listener")
	}
	return core.Int(addr.Port), nil
}

func tcpAcceptFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-accept", args, 1); err != nil {
		return nil, err
	}
	h, err := argHandle("tcp-accept", args, 0, handleTCPListener)
	if err != nil {
		return nil, err
	}
	conn, err := h.Resource.(net.Listener).Accept()
	if err != nil {
		return nil, fmt.Errorf("tcp-accept: %w", err)
	}
	return &core.Handle{Kind: handleTCPConn, Resource: conn}, nil
}

func tcpConnectFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-connect", args, 2); err != nil {
		return nil, err
	}
	host, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("tcp-connect: host must be a string")
	}
	port, ok := args[1].(core.Int)
	if !ok {
		return nil, fmt.Errorf("tcp-connect: port must be an int")
	}
	conn, err := net.Dial("tcp", net.JoinHostPort(string(host), strconv.Itoa(int(port))))
	if err != nil {
		return nil, fmt.Errorf("tcp-connect: %w", err)
	}
	return &core.Handle{Kind: handleTCPConn, Resource: conn}, nil
}

// tcpRecvFn reads one chunk from a connection, returning it as a string, or the
// atom :eof when the peer has closed the connection.
func tcpRecvFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-recv", args, 1); err != nil {
		return nil, err
	}
	h, err := argHandle("tcp-recv", args, 0, handleTCPConn)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, rerr := h.Resource.(net.Conn).Read(buf)
	if n > 0 {
		return core.String(buf[:n]), nil
	}
	if rerr == io.EOF {
		return core.Atom("eof"), nil
	}
	if rerr != nil {
		return nil, fmt.Errorf("tcp-recv: %w", rerr)
	}
	return core.String(""), nil
}

func tcpSendFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("tcp-send", args, 2); err != nil {
		return nil, err
	}
	h, err := argHandle("tcp-send", args, 0, handleTCPConn)
	if err != nil {
		return nil, err
	}
	data, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("tcp-send: data must be a string")
	}
	if _, err := h.Resource.(net.Conn).Write([]byte(data)); err != nil {
		return nil, fmt.Errorf("tcp-send: %w", err)
	}
	return core.Atom("ok"), nil
}

// closeFn closes any closable handle (listener or connection).
func closeFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("close", args, 1); err != nil {
		return nil, err
	}
	h, ok := args[0].(*core.Handle)
	if !ok {
		return nil, fmt.Errorf("close: argument must be a handle, got %T", args[0])
	}
	c, ok := h.Resource.(io.Closer)
	if !ok {
		return nil, fmt.Errorf("close: %s handle is not closable", h.Kind)
	}
	if err := c.Close(); err != nil {
		return nil, fmt.Errorf("close: %w", err)
	}
	return core.Atom("ok"), nil
}

// UDP is connectionless: one socket both receives and sends, and each datagram
// carries its peer address. udp-recv therefore returns {data address} and
// udp-send takes the peer address (the "host:port" string udp-recv handed back).

func udpOpenFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("udp-open", args, 1); err != nil {
		return nil, err
	}
	port, ok := args[0].(core.Int)
	if !ok {
		return nil, fmt.Errorf("udp-open: port must be an int")
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: int(port)})
	if err != nil {
		return nil, fmt.Errorf("udp-open: %w", err)
	}
	return &core.Handle{Kind: handleUDPSocket, Resource: conn}, nil
}

func udpPortFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("udp-port", args, 1); err != nil {
		return nil, err
	}
	h, err := argHandle("udp-port", args, 0, handleUDPSocket)
	if err != nil {
		return nil, err
	}
	return core.Int(h.Resource.(*net.UDPConn).LocalAddr().(*net.UDPAddr).Port), nil
}

// udpRecvFn receives one datagram, returning {data peer-address}.
func udpRecvFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("udp-recv", args, 1); err != nil {
		return nil, err
	}
	h, err := argHandle("udp-recv", args, 0, handleUDPSocket)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 65536)
	n, addr, rerr := h.Resource.(*net.UDPConn).ReadFromUDP(buf)
	if rerr != nil {
		return nil, fmt.Errorf("udp-recv: %w", rerr)
	}
	return core.Tuple{core.String(buf[:n]), core.String(addr.String())}, nil
}

func udpSendFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("udp-send", args, 3); err != nil {
		return nil, err
	}
	h, err := argHandle("udp-send", args, 0, handleUDPSocket)
	if err != nil {
		return nil, err
	}
	peer, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("udp-send: address must be a string")
	}
	data, ok := args[2].(core.String)
	if !ok {
		return nil, fmt.Errorf("udp-send: data must be a string")
	}
	addr, err := net.ResolveUDPAddr("udp", string(peer))
	if err != nil {
		return nil, fmt.Errorf("udp-send: %w", err)
	}
	if _, err := h.Resource.(*net.UDPConn).WriteToUDP([]byte(data), addr); err != nil {
		return nil, fmt.Errorf("udp-send: %w", err)
	}
	return core.Atom("ok"), nil
}

func fileReadFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("file-read", args, 1); err != nil {
		return nil, err
	}
	path, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("file-read: path must be a string")
	}
	data, err := os.ReadFile(string(path))
	if err != nil {
		return nil, fmt.Errorf("file-read: %w", err)
	}
	return core.String(data), nil
}

// fileWriteFn writes (creating or truncating) a file, returning :ok.
func fileWriteFn(args []Value, _ *Env) (Value, error) {
	if err := wantArgs("file-write", args, 2); err != nil {
		return nil, err
	}
	path, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("file-write: path must be a string")
	}
	data, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("file-write: data must be a string")
	}
	if err := os.WriteFile(string(path), []byte(data), 0o644); err != nil {
		return nil, fmt.Errorf("file-write: %w", err)
	}
	return core.Atom("ok"), nil
}
