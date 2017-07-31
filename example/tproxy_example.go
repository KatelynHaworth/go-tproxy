package main

import (
	"github.com/LiamHaworth/go-tproxy"
	"io"
	"log"
	"net"
	"sync"
)

func main() {
	log.Println("Starting GoLang TProxy example")

	bindAddr := &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 8080}
	log.Printf("Attempting to bind listener on: %s", bindAddr.String())

	listener, err := tproxy.ListenTCP("tcp", bindAddr)
	if err != nil {
		log.Fatalf("Encountered error while binding listener: %s", err)
		return
	}

	log.Println("Listener bound successfully, now accepting connections")
	for {
		conn, err := listener.AcceptTProxy()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				log.Printf("Temporary error while accepting connection: %s", netErr)
			}

			log.Fatalf("Unrecoverable error while accepting connection: %s", err)
			return
		}

		go handleConn(conn)
	}
}

func handleConn(conn *tproxy.TProxyConn) {
	log.Printf("Accepting connection from %s with destination of %s", conn.RemoteAddr().String(), conn.LocalAddr().String())

	remoteConn, err := conn.DialOriginalDestination(false)
	if err != nil {
		log.Printf("Failed to connect to original destination [%s]: %s", conn.LocalAddr().String(), err)
	} else {
		defer remoteConn.Close()
		defer conn.Close()
	}

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
