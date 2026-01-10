package netutil

import (
	"context"
	"fmt"
	"net"
	"time"
)

// SafeDialer is a net.Dialer that prevents connections to private, loopback,
// and link-local IP addresses.
type SafeDialer struct {
	// Timeout is the maximum amount of time a dial will wait for
	// a connect to complete. If Deadline is also set, it may fail
	// earlier.
	//
	// The default is no timeout.
	//
	// When using TCP and dialing a host name with multiple IP
	// addresses, the timeout may be divided between them.
	//
	// With or without a timeout, the operating system may impose
	// its own earlier timeout. For instance, TCP timeouts are
	// often around 3 minutes.
	Timeout time.Duration

	// Deadline is the absolute point in time after which dials
	// will fail. If Timeout is also set, it may fail earlier.
	// Zero means no deadline, or dependent on the operating system
	// as with the Timeout option.
	Deadline time.Time
}

// DialContext connects to the address on the named network using the provided
// context. It prevents connections to private, loopback, and link-local IPs.
func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// If splitting fails, it might be because the address doesn't have a port.
		// In this case, we can treat the whole address as the host and port as empty.
		host = address
		port = ""
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve host: %w", err)
	}

	var firstPublicIP net.IP
	for _, ip := range ips {
		if isPrivate(ip) {
			return nil, fmt.Errorf("connection to private IP %s is not allowed", ip)
		}
		if firstPublicIP == nil {
			firstPublicIP = ip
		}
	}

	if firstPublicIP == nil {
		return nil, fmt.Errorf("no public IP found for host: %s", host)
	}

	// Connect directly to the validated IP address to prevent TOCTOU
	dialer := &net.Dialer{
		Timeout:  d.Timeout,
		Deadline: d.Deadline,
	}
	// Re-join host and port if a port was specified
	connectAddress := firstPublicIP.String()
	if port != "" {
		connectAddress = net.JoinHostPort(connectAddress, port)
	}

	return dialer.DialContext(ctx, network, connectAddress)
}

// isPrivate checks if an IP address is private, loopback, or link-local.
func isPrivate(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}
