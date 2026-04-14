package pipery

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// fileSink appends one JSON line per log entry to a local file.
type fileSink struct {
	file *os.File
	path string
}

// newFileSink opens the file in append mode so existing logs are preserved.
func newFileSink(path string) (*fileSink, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	return &fileSink{file: file, path: path}, nil
}

// Name returns a human-readable sink identifier for error messages.
func (s *fileSink) Name() string {
	return s.path
}

// Write appends raw bytes to the file.
func (s *fileSink) Write(payload []byte) error {
	_, err := s.file.Write(payload)
	return err
}

// Close releases the underlying file descriptor.
func (s *fileSink) Close() error {
	return s.file.Close()
}

// syslogSink sends log entries to a remote syslog listener over TCP or UDP.
//
// The mutex protects the lazy connection and reconnect logic so concurrent
// writes cannot corrupt shared state.
type syslogSink struct {
	mu       sync.Mutex
	network  string
	address  string
	tag      string
	hostname string
	conn     net.Conn
}

// newSyslogSink parses the target URL and captures a hostname for later log
// formatting.
func newSyslogSink(target, tag string) (*syslogSink, error) {
	network, address, err := parseSyslogTarget(target)
	if err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}

	return &syslogSink{
		network:  network,
		address:  address,
		tag:      tag,
		hostname: hostname,
	}, nil
}

// Name returns a descriptive label used in error messages.
func (s *syslogSink) Name() string {
	return fmt.Sprintf("syslog(%s://%s)", s.network, s.address)
}

// Write formats the payload like a syslog message and sends it.
//
// If a write fails, we close the connection, reconnect once, and retry. This
// gives us a simple recovery path for temporary network hiccups.
func (s *syslogSink) Write(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureConn(); err != nil {
		return err
	}

	if err := s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}

	_, err := s.conn.Write(s.format(payload))
	if err == nil {
		return nil
	}

	_ = s.conn.Close()
	s.conn = nil

	if err := s.ensureConn(); err != nil {
		return err
	}

	if err := s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}

	_, err = s.conn.Write(s.format(payload))
	return err
}

// Close shuts down any open network connection.
func (s *syslogSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return nil
	}

	err := s.conn.Close()
	s.conn = nil
	return err
}

// ensureConn lazily opens the network connection only when it is first needed.
func (s *syslogSink) ensureConn() error {
	if s.conn != nil {
		return nil
	}

	conn, err := net.DialTimeout(s.network, s.address, 2*time.Second)
	if err != nil {
		return err
	}

	s.conn = conn
	return nil
}

// format wraps the JSON payload in a simple syslog envelope.
//
// This implementation is intentionally lightweight: the structured JSON remains
// the main body, and syslog adds transport-friendly metadata around it.
func (s *syslogSink) format(payload []byte) []byte {
	body := strings.TrimSpace(string(payload))
	timestamp := time.Now().Format("Jan _2 15:04:05")

	return []byte(fmt.Sprintf("<14>%s %s %s[%d]: %s\n", timestamp, s.hostname, s.tag, os.Getpid(), body))
}

// parseSyslogTarget validates and normalizes a syslog URL like:
//
//	udp://127.0.0.1:514
//	tcp://log-server.example.com
//
// If the port is omitted, we default to the standard syslog port 514.
func parseSyslogTarget(target string) (string, string, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return "", "", err
	}

	switch parsed.Scheme {
	case "tcp", "udp":
	default:
		return "", "", fmt.Errorf("unsupported syslog scheme %q, expected tcp:// or udp://", parsed.Scheme)
	}

	address := parsed.Host
	if address == "" {
		return "", "", fmt.Errorf("syslog target %q is missing a host", target)
	}

	if _, _, err := net.SplitHostPort(address); err != nil {
		if addrErr, ok := err.(*net.AddrError); ok && strings.Contains(addrErr.Err, "missing port in address") {
			// Allow a friendlier URL by filling in the standard port automatically.
			address = net.JoinHostPort(address, "514")
		} else {
			return "", "", err
		}
	}

	return parsed.Scheme, address, nil
}
