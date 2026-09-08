// Package protocol defines the CoreNet 0.1 wire types shared by corenetd and
// the corenet CLI. Everything here is plain JSON over HTTP.
package protocol

import (
	"fmt"
	"strings"
)

// Version is the CoreNet protocol version implemented by this build.
const Version = "0.2"

// TLD is the CoreNet namespace. Names outside it are not CoreNet names.
const TLD = "core"

// Service sources. A service comes from the local configuration file, from a
// runtime registration, from a Docker container, or from another node (in
// which case Source is that node's ID).
const (
	SourceConfig  = "config"
	SourceRuntime = "runtime"
	SourceDocker  = "docker"
)

// Service is one reachable CoreNet service.
type Service struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Source  string `json:"source,omitempty"`
}

// Addr returns the backend address a proxy should dial.
func (s Service) Addr() string {
	return fmt.Sprintf("%s:%d", s.Address, s.Port)
}

// Node is a CoreNet node: this one, or a statically configured peer.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Local    bool   `json:"local"`
	Online   bool   `json:"online"`
	Services int    `json:"services"`
	LastSeen int64  `json:"last_seen,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Listeners reports the addresses corenetd is serving on.
type Listeners struct {
	DNS     string `json:"dns,omitempty"`
	HTTP    string `json:"http,omitempty"`
	Peer    string `json:"peer,omitempty"`
	Control string `json:"control,omitempty"`
}

// Status is the answer to GET /v1/status.
type Status struct {
	Version   string    `json:"version"`
	NodeID    string    `json:"node_id"`
	NodeName  string    `json:"node_name,omitempty"`
	Uptime    int64     `json:"uptime_seconds"`
	Listeners Listeners `json:"listeners"`
	Services  int       `json:"services"`
	Nodes     int       `json:"nodes"`
}

// Info is the answer to GET /v1/info on the peer listener. It is deliberately
// smaller than Status: other nodes have no business knowing our listeners.
type Info struct {
	Version  string `json:"version"`
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name,omitempty"`
	Services int    `json:"services"`
}

// Conflict is a name this node knows but refuses to route, because more than
// one Docker container claims it.
type Conflict struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ServiceList is the answer to GET /v1/services.
//
// Conflicts are reported on the control socket only: a name this node cannot
// route is not a name it offers to other nodes.
type ServiceList struct {
	Services  []Service  `json:"services"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
}

// NodeList is the answer to GET /v1/nodes.
type NodeList struct {
	Nodes []Node `json:"nodes"`
}

// Error codes used across the CoreNet APIs.
const (
	CodeBadRequest = "bad_request"
	CodeNotFound   = "not_found"
	CodeConflict   = "conflict"
	CodeBadGateway = "bad_gateway"
	CodeInternal   = "internal"
)

// ErrorBody carries a machine-readable code and a human-readable message.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the body returned with every non-2xx JSON response.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

func (e ErrorResponse) String() string {
	return fmt.Sprintf("%s: %s", e.Error.Code, e.Error.Message)
}

// NormalizeName lowercases a CoreNet name and strips a trailing dot, so that
// "Wiki.Core." and "wiki.core" are the same key.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// ValidateName reports whether name is a usable CoreNet name. It must be a
// normalized name in the .core namespace with ordinary DNS labels.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if name != NormalizeName(name) {
		return fmt.Errorf("name %q is not normalized (lowercase, no trailing dot)", name)
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 || labels[len(labels)-1] != TLD {
		return fmt.Errorf("name %q is not in the .%s namespace", name, TLD)
	}
	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return fmt.Errorf("name %q: %w", name, err)
		}
	}
	return nil
}

func validateLabel(label string) error {
	if label == "" {
		return fmt.Errorf("empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("label %q is longer than 63 characters", label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("label %q starts or ends with a hyphen", label)
	}
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return fmt.Errorf("label %q contains an invalid character %q", label, r)
		}
	}
	return nil
}
