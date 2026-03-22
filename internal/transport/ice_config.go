package transport

import (
	"net"
	"os"
	"strings"

	"github.com/pion/webrtc/v4"
)

var defaultSTUNURLs = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
	"stun:stun2.l.google.com:19302",
	"stun:stun3.l.google.com:19302",
	"stun:stun4.l.google.com:19302",
	"stun:stun.cloudflare.com:3478",
}

var defaultTURNURLs = []string{
	"turn:openrelay.metered.ca:80",
	"turn:openrelay.metered.ca:80?transport=tcp",
	"turn:openrelay.metered.ca:443",
	"turn:openrelay.metered.ca:443?transport=tcp",
	"turns:openrelay.metered.ca:443?transport=tcp",
}

const (
	defaultTURNUsername   = "openrelayproject"
	defaultTURNCredential = "openrelayproject"
)

func ConfiguredICEServers() []webrtc.ICEServer {
	stunURLs := parseCSVEnv("WEBRTC_STUN_URLS", defaultSTUNURLs)
	turnURLs := parseCSVEnv("WEBRTC_TURN_URLS", defaultTURNURLs)
	turnUsername := envOrDefault("WEBRTC_TURN_USERNAME", defaultTURNUsername)
	turnCredential := envOrDefault("WEBRTC_TURN_CREDENTIAL", defaultTURNCredential)

	servers := make([]webrtc.ICEServer, 0, len(stunURLs)+1)
	for _, url := range stunURLs {
		servers = append(servers, webrtc.ICEServer{URLs: []string{url}})
	}

	if len(turnURLs) > 0 {
		servers = append(servers, webrtc.ICEServer{
			URLs:       turnURLs,
			Username:   turnUsername,
			Credential: turnCredential,
		})
	}

	return servers
}

func ShouldForceRelayDefault(hostName string) bool {
	host := normalizeHost(hostName)
	if host == "" {
		return false
	}

	if parseBoolEnv("WEBRTC_FORCE_RELAY") {
		return true
	}

	if host == "localhost" || host == "127.0.0.1" {
		return false
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() {
			return false
		}
		return true
	}

	return strings.Contains(host, ".ngrok-free.app") ||
		strings.Contains(host, ".ngrok-free.dev") ||
		strings.Contains(host, ".ngrok.app") ||
		strings.Contains(host, ".ngrok.dev") ||
		strings.Contains(host, ".app.github.dev") ||
		strings.Contains(host, ".github.dev")
}

func normalizeHost(hostName string) string {
	host := strings.ToLower(strings.TrimSpace(hostName))
	if host == "" {
		return ""
	}

	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return parsedHost
	}

	return host
}

func parseCSVEnv(name string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return append([]string(nil), fallback...)
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}

	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}

	return values
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func parseBoolEnv(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
