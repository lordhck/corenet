// Package discovery turns running Docker containers into CoreNet services.
//
// Docker is a local discovery mechanism, never part of the CoreNet network
// protocol. Everything here is read-only: the client asks the Docker Engine
// API what is running and nothing else.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"corenet/pkg/protocol"
)

// The Engine API version this client speaks. Docker keeps older versions
// working, so pinning an old one is the compatible choice.
const apiVersion = "v1.41"

const (
	requestTimeout  = 5 * time.Second
	maxResponseSize = 8 << 20
)

// CoreNet container labels.
const (
	LabelName    = "corenet.name"
	LabelService = "corenet.service"
	LabelPort    = "corenet.port"
)

// ServiceHTTP is the only service type CoreNet 0.2 defines.
const ServiceHTTP = "http"

// Client is a minimal read-only Docker Engine API client.
type Client struct {
	http   *http.Client
	socket string
}

// NewClient returns a client for the Docker socket at path.
func NewClient(socket string) *Client {
	return &Client{
		socket: socket,
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// container is the part of the Docker container list this package needs.
type container struct {
	ID              string            `json:"Id"`
	Names           []string          `json:"Names"`
	State           string            `json:"State"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// name is the container's display name, for log messages.
func (c container) name() string {
	if len(c.Names) > 0 && len(c.Names[0]) > 1 {
		return c.Names[0][1:] // Docker prefixes container names with "/"
	}
	if len(c.ID) > 12 {
		return c.ID[:12]
	}
	return c.ID
}

// listContainers returns the running containers.
func (c *Client) listContainers(ctx context.Context) ([]container, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := "http://docker/" + apiVersion + "/containers/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker returned %s", resp.Status)
	}
	var containers []container
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// service converts one container into a CoreNet service. Container metadata
// is untrusted input: every label is validated, and a container that does not
// describe a usable service is skipped with a reason.
func service(c container, preferredNetwork string) (protocol.Service, error) {
	name := protocol.NormalizeName(c.Labels[LabelName])
	if name == "" {
		return protocol.Service{}, errSkip
	}
	if err := protocol.ValidateName(name); err != nil {
		return protocol.Service{}, err
	}

	switch kind := c.Labels[LabelService]; kind {
	case ServiceHTTP:
	case "":
		return protocol.Service{}, fmt.Errorf("no %s label", LabelService)
	default:
		return protocol.Service{}, fmt.Errorf("unsupported %s %q", LabelService, kind)
	}

	rawPort, ok := c.Labels[LabelPort]
	if !ok {
		return protocol.Service{}, fmt.Errorf("no %s label", LabelPort)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return protocol.Service{}, fmt.Errorf("invalid %s %q", LabelPort, rawPort)
	}

	address, err := address(c, preferredNetwork)
	if err != nil {
		return protocol.Service{}, err
	}
	return protocol.Service{
		Name:    name,
		Address: address,
		Port:    port,
		Source:  protocol.SourceDocker,
	}, nil
}

// errSkip marks a container that is not trying to be a CoreNet service, so it
// is passed over silently rather than reported as a problem.
var errSkip = fmt.Errorf("not a corenet container")

// address picks the container address corenetd should dial. Selection is
// deterministic: the configured network, then the container's only network,
// then the networks sorted by name.
func address(c container, preferredNetwork string) (string, error) {
	networks := c.NetworkSettings.Networks
	if preferredNetwork != "" {
		if net, ok := networks[preferredNetwork]; ok && net.IPAddress != "" {
			return net.IPAddress, nil
		}
		return "", fmt.Errorf("no address on network %q", preferredNetwork)
	}

	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if ip := networks[name].IPAddress; ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no reachable address")
}
