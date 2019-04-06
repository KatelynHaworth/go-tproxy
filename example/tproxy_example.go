package main

import (
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/LiamHaworth/go-tproxy"
)

var (
	// tcpListener represents the TCP
	// listening socket that will receive
	// TCP connections from TProxy
	tcpListener net.Listener

	// udpListener represents tje UDP
	// listening socket that will receive
	// UDP packets from TProxy
	udpListener *net.UDPConn
)

// main will initialize the TProxy
// handling application
func main() {
	log.Println("Starting GoLang TProxy example")
	var err error

	log.Println("Binding TCP TProxy listener to 0.0.0.0:8080")
	tcpListener, err = tproxy.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 8080})
	if err != nil {
		log.Fatalf("Encountered error while binding listener: %s", err)
		return
	}

	defer tcpListener.Close()
	go listenTCP()

	log.Println("Binding UDP TProxy listener to 0.0.0.0:8080")
	udpListener, err = tproxy.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 8080})
	if err != nil {
		log.Fatalf("Encountered error while binding UDP listener: %s", err)
		return
	}

	defer udpListener.Close()
	go listenUDP()

	interruptListener := make(chan os.Signal)
	signal.Notify(interruptListener, os.Interrupt)
	<-interruptListener

	log.Println("TProxy listener closing")
}

// listenUDP runs in a routine to
// accept UDP connections and hand them
// off into their own routines for handling
func listenUDP() {
	for {
		buff := make([]byte, 1024)
		n, srcAddr, dstAddr, err := tproxy.ReadFromUDP(udpListener, buff)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				log.Printf("Temporary error while reading data: %s", netErr)
			}

			log.Fatalf("Unrecoverable error while reading data: %s", err)
			return
		}

		log.Printf("Accepting UDP connection from %s with destination of %s", srcAddr.String(), dstAddr.String())
		go handleUDPConn(buff[:n], srcAddr, dstAddr)
	}
}

// listenTCP runs in a routine to
// accept TCP connections and hand them
// off into their own routines for handling
func listenTCP() {
	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				log.Printf("Temporary error while accepting connection: %s", netErr)
			}

			log.Fatalf("Unrecoverable error while accepting connection: %s", err)
			return
		}

		go handleTCPConn(conn)
	}
}

// handleUDPConn will open a connection
// to the original destination pretending
// to be the client. It will when right
// the received data to the remote host
// and wait a few seconds for any possible
// response data
func handleUDPConn(data []byte, srcAddr, dstAddr *net.UDPAddr) {
	log.Printf("Accepting UDP connection from %s with destination of %s", srcAddr, dstAddr)

	localConn, err := tproxy.DialUDP("udp", dstAddr, srcAddr)
	if err != nil {
		log.Printf("Failed to connect to original UDP source [%s]: %s", srcAddr.String(), err)
		return
	}
	defer localConn.Close()

	remoteConn, err := tproxy.DialUDP("udp", srcAddr, dstAddr)
	if err != nil {
		log.Printf("Failed to connect to original UDP destination [%s]: %s", dstAddr.String(), err)
		return
	}
	defer remoteConn.Close()

	bytesWritten, err := remoteConn.Write(data)
	if err != nil {
		log.Printf("Encountered error while writing to remote [%s]: %s", remoteConn.RemoteAddr(), err)
		return
	} else if bytesWritten < len(data) {
		log.Printf("Not all bytes [%d < %d] in buffer written to remote [%s]", bytesWritten, len(data), remoteConn.RemoteAddr())
		return
	}

	data = make([]byte, 1024)
	remoteConn.SetReadDeadline(time.Now().Add(2 * time.Second)) // Add deadline to ensure it doesn't block forever
	bytesRead, err := remoteConn.Read(data)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return
		}

		log.Printf("Encountered error while reading from remote [%s]: %s", remoteConn.RemoteAddr(), err)
		return
	}

	bytesWritten, err = localConn.Write(data)
	if err != nil {
		log.Printf("Encountered error while writing to local [%s]: %s", localConn.RemoteAddr(), err)
		return
	} else if bytesWritten < bytesRead {
		log.Printf("Not all bytes [%d < %d] in buffer written to locoal [%s]", bytesWritten, len(data), remoteConn.RemoteAddr())
		return
	}
}

// handleTCPConn will open a connection
// to the original destination pretending
// to be the client. From there it will setup
// two routines to stream data between the
// connections
func handleTCPConn(conn net.Conn) {
	log.Printf("Accepting TCP connection from %s with destination of %s", conn.RemoteAddr().String(), conn.LocalAddr().String())
	defer conn.Close()
	
	remoteConn, err := conn.(*tproxy.Conn).DialOriginalDestination(false)
	if err != nil {
		log.Printf("Failed to connect to original destination [%s]: %s", conn.LocalAddr().String(), err)
		return
	} 
	defer remoteConn.Close()

	var streamWait sync.WaitGroup
	streamWait.Add(2)

	streamConn := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src)
		streamWait.Done()
	}

	go streamConn(remoteConn, conn)
	go streamConn(conn, remoteConn)

	streamWait.Wait()
}
