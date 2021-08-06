package udp

import (
	"fmt"
	"io"
	"net"

	"github.com/pires/go-proxyproto"
	"github.com/traefik/traefik/v2/pkg/config/dynamic"
	"github.com/traefik/traefik/v2/pkg/log"
)

// Proxy is a reverse-proxy implementation of the Handler interface.
type Proxy struct {
	// TODO: maybe optimize by pre-resolving it at proxy creation time
	target        string
	proxyProtocol *dynamic.ProxyProtocol
}

// NewProxy creates a new Proxy.
func NewProxy(address string, proxyProtocol *dynamic.ProxyProtocol) (*Proxy, error) {
	if proxyProtocol != nil && (proxyProtocol.Version < 1 || proxyProtocol.Version > 2) {
		return nil, fmt.Errorf("unknown proxyProtocol version: %d", proxyProtocol.Version)
	}

	return &Proxy{target: address, proxyProtocol: proxyProtocol}, nil
}

// ServeUDP implements the Handler interface.
func (p *Proxy) ServeUDP(conn *Conn) {
	log.WithoutContext().Debugf("Handling UDP stream from %s to %s", conn.rAddr, p.target)

	// needed because of e.g. server.trackedConnection
	defer conn.Close()

	connBackend, err := net.Dial("udp", p.target)
	if err != nil {
		log.WithoutContext().Errorf("Error while dialing backend: %v", err)
		return
	}

	// maybe not needed, but just in case
	defer connBackend.Close()

	if p.proxyProtocol != nil && p.proxyProtocol.Version > 0 && p.proxyProtocol.Version < 3 {
		header := proxyproto.HeaderProxyFromAddrs(byte(p.proxyProtocol.Version), conn.RemoteAddr(), conn.LocalAddr())
		if _, err := header.WriteTo(connBackend); err != nil {
			log.WithoutContext().Errorf("Error while writing proxy protocol headers to backend connection: %v", err)
			return
		}
	}

	errChan := make(chan error)
	go connCopy(conn, connBackend, errChan)
	go connCopy(connBackend, conn, errChan)

	err = <-errChan
	if err != nil {
		log.WithoutContext().Errorf("Error while handling UDP stream: %v", err)
	}

	<-errChan
}

func connCopy(dst io.WriteCloser, src io.Reader, errCh chan error) {
	// The buffer is initialized to the maximum UDP datagram size,
	// to make sure that the whole UDP datagram is read or written atomically (no data is discarded).
	buffer := make([]byte, maxDatagramSize)

	_, err := io.CopyBuffer(dst, src, buffer)
	errCh <- err

	if err := dst.Close(); err != nil {
		log.WithoutContext().Debugf("Error while terminating UDP stream: %v", err)
	}
}
