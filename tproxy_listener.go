// +build linux

package tproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
)

// TProxyListener describes a TCP Listener
// with the Linux IP_TRANSPARENT option defined
// on the listening socket
type TProxyListener struct {
	*net.TCPListener
}

// AcceptTProxy will accept a TCP connection
// and wrap it to a TProxy connection to provide
// TProxy functionality
func (listener *TProxyListener) AcceptTProxy() (*TProxyConn, error) {
	tcpConn, err := listener.AcceptTCP()
	if err != nil {
		return nil, err
	}

	return &TProxyConn{TCPConn: tcpConn}, nil
}

// ListenTCP will construct a new TCP listener
// socket with the Linux IP_TRANSPARENT option
// set on the underlying socket
func ListenTCP(network string, laddr *net.TCPAddr) (*TProxyListener, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: net.UnknownNetworkError(network)}
	}

	if laddr == nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: errors.New("local bind address is nil")}
	}

	socketAddr, err := addrToSocketAddr(laddr)
	if err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("build socket address: %s", err)}
	}

	fileDescriptor, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("socket open: %s", err)}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("set socket option: SO_REUSEADDR: %s", err)}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("set socket option: IP_TRANSPARENT: %s", err)}
	}

	if err = syscall.SetNonblock(fileDescriptor, true); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("set socket option: SO_NONBLOCK: %s", err)}
	}

	if err = syscall.Bind(fileDescriptor, socketAddr); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("socket bind: %s", err)}
	}

	if err = syscall.Listen(fileDescriptor, syscall.SOMAXCONN); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("socket listen: %s", err)}
	}

	socket, err := net.FileListener(os.NewFile(uintptr(fileDescriptor), fmt.Sprintf("net-tcp-listener-%s", laddr.String())))
	if err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "listen", Net: network, Source: nil, Addr: laddr, Err: fmt.Errorf("convert file descriptor to listener: %s", err)}
	}

	return &TProxyListener{socket.(*net.TCPListener)}, nil
}

// addToSockerAddr will convert a TCPAddr
// into a Sockaddr that may be used when
// connecting and binding sockets
func addrToSocketAddr(addr *net.TCPAddr) (syscall.Sockaddr, error) {
	switch {
	case addr.IP.To4() != nil:
		ip := [4]byte{}
		copy(ip[:], addr.IP.To4())

		return &syscall.SockaddrInet4{Addr: ip, Port: addr.Port}, nil

	default:
		ip := [16]byte{}
		copy(ip[:], addr.IP.To16())

		zoneId, err := strconv.ParseUint(addr.Zone, 10, 32)
		if err != nil {
			return nil, err
		}

		return &syscall.SockaddrInet6{Addr: ip, Port: addr.Port, ZoneId: uint32(zoneId)}, nil
	}
}
