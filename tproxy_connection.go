package tproxy

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
)

// +build linux

// TProxyConn describes a connection
// accepted by the TProxy listener.
//
// It is simply a TCP connection with
// the ability to dial a connection to
// the original destination while assuming
// the IP address of the client
type TProxyConn struct {
	*net.TCPConn
}

// DialOriginalDestination will open a
// TCP connection to the original destination
// that the client was trying to connect to before
// being intercepted.
//
// When `dontAssumeRemote` is false, the connection will
// originate from the IP address and port that the client
// used when making the connection. Otherwise, when false,
// the connection will originate from an IP address and port
// assigned by the Linux kernel that is owned by the
// operating system
func (conn *TProxyConn) DialOriginalDestination(dontAssumeRemote bool) (*net.TCPConn, error) {
	remoteSocketAddress, err := addrToSocketAddr(conn.LocalAddr().(*net.TCPAddr))
	if err != nil {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("build destination socket address: %s", err)}
	}

	localSocketAddress, err := addrToSocketAddr(conn.RemoteAddr().(*net.TCPAddr))
	if err != nil {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("build local socket address: %s", err)}
	}

	fileDescriptor, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket open: %s", err)}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: SO_REUSEADDR: %s", err)}
	}

	if err = syscall.SetsockoptInt(fileDescriptor, syscall.SOL_IP, syscall.IP_TRANSPARENT, 1); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: IP_TRANSPARENT: %s", err)}
	}

	if err = syscall.SetNonblock(fileDescriptor, true); err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("set socket option: SO_NONBLOCK: %s", err)}
	}

	if !dontAssumeRemote {
		if err = syscall.Bind(fileDescriptor, localSocketAddress); err != nil {
			syscall.Close(fileDescriptor)
			return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket bind: %s", err)}
		}
	}

	if err = syscall.Connect(fileDescriptor, remoteSocketAddress); err != nil && !strings.Contains(err.Error(), "operation now in progress") {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("socket connect: %s", err)}
	}

	remoteConn, err := net.FileConn(os.NewFile(uintptr(fileDescriptor), fmt.Sprintf("net-tcp-dial-%s", conn.LocalAddr().String())))
	if err != nil {
		syscall.Close(fileDescriptor)
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("convert file descriptor to connection: %s", err)}
	}

	return remoteConn.(*net.TCPConn), nil
}
