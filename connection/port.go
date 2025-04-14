package connection

import (
	"fmt"
	"net"
)

// GetFreePort asks the kernel for a free open port that is ready to use.
// It binds to TCP port 0 on the specified host (defaulting to "127.0.0.1"),
// retrieves the assigned port, and then closes the listener.
func GetFreePort(host string) (int, error) {
	if host == "" {
		host = "127.0.0.1" // Default to localhost
	}
	addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("failed to resolve tcp address: %w", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("failed to listen on tcp port 0: %w", err)
	}
	defer l.Close() // Ensure listener is closed

	// Retrieve the port assigned by the kernel
	port := l.Addr().(*net.TCPAddr).Port
	if port == 0 {
		// This should theoretically not happen if ListenTCP succeeds
		return 0, fmt.Errorf("kernel assigned port 0 unexpectedly")
	}

	return port, nil
}
