// Command corenet is the CoreNet command line interface.
//
// It talks to the local corenetd over its Unix control socket and configures
// this machine to resolve .core names.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"corenet/internal/config"
	"corenet/pkg/protocol"
)

const usage = `corenet ` + protocol.Version + ` - CoreNet command line interface

Usage:
  corenet connect                            resolve .core names on this machine
  corenet disconnect                         stop resolving .core names
  corenet status                             show the local node

  corenet service list                       list every resolvable service
  corenet service add <name> <address> <port>  register a service
  corenet service remove <name>              remove a registered service

  corenet resolve <name>                     look up a .core name
  corenet node list                          list this node and its peers
  corenet node info <id>                     show one node

Options:
  --config <path>   configuration file (default: the corenetd search path)
  --socket <path>   control socket (default: from the configuration)
  --json            print the raw API response
`

type options struct {
	config string
	socket string
	json   bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "corenet: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var opts options
	positional, err := parseArgs(&opts, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		fmt.Print(usage)
		return nil
	}

	switch positional[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "version":
		fmt.Printf("corenet %s\n", protocol.Version)
		return nil
	case "connect":
		return connect(&opts)
	case "disconnect":
		return disconnect()
	case "status":
		return status(&opts)
	case "service":
		return service(&opts, positional[1:])
	case "resolve":
		return resolve(&opts, positional[1:])
	case "node":
		return node(&opts, positional[1:])
	default:
		return fmt.Errorf("unknown command %q (try: corenet help)", positional[0])
	}
}

// parseArgs pulls the options out of args, wherever they appear, and returns
// the positional arguments in order.
func parseArgs(opts *options, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		fs := flag.NewFlagSet("corenet", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&opts.config, "config", opts.config, "")
		fs.StringVar(&opts.socket, "socket", opts.socket, "")
		fs.BoolVar(&opts.json, "json", opts.json, "")
		if err := fs.Parse(args); err != nil {
			if len(positional) == 0 && (args[0] == "-h" || args[0] == "--help") {
				return []string{"help"}, nil
			}
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

func status(opts *options) error {
	c, err := newClient(opts)
	if err != nil {
		return err
	}
	var st protocol.Status
	if err := c.get("/v1/status", &st, opts.json); err != nil || opts.json {
		return err
	}

	name := st.NodeName
	if name == "" {
		name = st.NodeID
	}
	w := newTable()
	fmt.Fprintf(w, "CoreNet\t%s\n", st.Version)
	fmt.Fprintf(w, "node\t%s (%s)\n", name, st.NodeID)
	fmt.Fprintf(w, "uptime\t%s\n", time.Duration(st.Uptime)*time.Second)
	fmt.Fprintf(w, "resolution\t%s\n", resolutionState())
	fmt.Fprintf(w, "dns\t%s\n", orNone(st.Listeners.DNS))
	fmt.Fprintf(w, "http\t%s\n", orNone(st.Listeners.HTTP))
	fmt.Fprintf(w, "peer\t%s\n", orNone(st.Listeners.Peer))
	fmt.Fprintf(w, "control\t%s\n", st.Listeners.Control)
	fmt.Fprintf(w, "services\t%d\n", st.Services)
	fmt.Fprintf(w, "nodes\t%d\n", st.Nodes)
	return w.Flush()
}

func service(opts *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: corenet service list|add|remove")
	}
	c, err := newClient(opts)
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		var list protocol.ServiceList
		if err := c.get("/v1/services", &list, opts.json); err != nil || opts.json {
			return err
		}
		if len(list.Services) == 0 {
			fmt.Println("no services")
			return nil
		}
		w := newTable()
		fmt.Fprintln(w, "NAME\tADDRESS\tSOURCE")
		for _, svc := range list.Services {
			fmt.Fprintf(w, "%s\t%s\t%s\n", svc.Name, svc.Addr(), svc.Source)
		}
		return w.Flush()

	case "add":
		if len(args) != 4 {
			return fmt.Errorf("usage: corenet service add <name> <address> <port>")
		}
		port, err := strconv.Atoi(args[3])
		if err != nil {
			return fmt.Errorf("invalid port %q", args[3])
		}
		svc := protocol.Service{Name: args[1], Address: args[2], Port: port}
		var added protocol.Service
		if err := c.post("/v1/services", svc, &added, opts.json); err != nil || opts.json {
			return err
		}
		fmt.Printf("registered %s -> %s\n", added.Name, added.Addr())
		return nil

	case "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: corenet service remove <name>")
		}
		name := protocol.NormalizeName(args[1])
		if err := c.delete("/v1/services/" + name); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", name)
		return nil

	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

func resolve(opts *options, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: corenet resolve <name>")
	}
	c, err := newClient(opts)
	if err != nil {
		return err
	}
	var svc protocol.Service
	path := "/v1/resolve?name=" + protocol.NormalizeName(args[0])
	if err := c.get(path, &svc, opts.json); err != nil || opts.json {
		return err
	}
	fmt.Printf("%s -> %s (%s)\n", svc.Name, svc.Addr(), svc.Source)
	return nil
}

func node(opts *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: corenet node list|info")
	}
	c, err := newClient(opts)
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		var list protocol.NodeList
		if err := c.get("/v1/nodes", &list, opts.json); err != nil || opts.json {
			return err
		}
		w := newTable()
		fmt.Fprintln(w, "ID\tNAME\tENDPOINT\tSTATE\tSERVICES")
		for _, n := range list.Nodes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n",
				orNone(n.ID), orNone(n.Name), orNone(n.Endpoint), nodeState(n), n.Services)
		}
		return w.Flush()

	case "info":
		if len(args) != 2 {
			return fmt.Errorf("usage: corenet node info <id>")
		}
		var n protocol.Node
		if err := c.get("/v1/nodes/"+args[1], &n, opts.json); err != nil || opts.json {
			return err
		}
		w := newTable()
		fmt.Fprintf(w, "id\t%s\n", orNone(n.ID))
		fmt.Fprintf(w, "name\t%s\n", orNone(n.Name))
		fmt.Fprintf(w, "endpoint\t%s\n", orNone(n.Endpoint))
		fmt.Fprintf(w, "state\t%s\n", nodeState(n))
		fmt.Fprintf(w, "services\t%d\n", n.Services)
		if n.LastSeen != 0 {
			fmt.Fprintf(w, "last seen\t%s\n", time.Unix(n.LastSeen, 0).Format(time.RFC3339))
		}
		if n.Error != "" {
			fmt.Fprintf(w, "error\t%s\n", n.Error)
		}
		return w.Flush()

	default:
		return fmt.Errorf("unknown node command %q", args[0])
	}
}

func nodeState(n protocol.Node) string {
	switch {
	case n.Local:
		return "local"
	case n.Online:
		return "online"
	default:
		return "offline"
	}
}

func orNone(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func newTable() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
}

// client speaks the control API over the Unix socket.
type client struct {
	http   *http.Client
	socket string
}

func newClient(opts *options) (*client, error) {
	socket := opts.socket
	if socket == "" {
		cfg, err := config.Load(opts.config)
		if err != nil {
			if os.IsNotExist(err) && opts.config == "" {
				cfg = config.Default()
			} else {
				return nil, err
			}
		}
		socket = cfg.Listen.Control
	}
	return &client{
		socket: socket,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
	}, nil
}

func (c *client) get(path string, out any, raw bool) error {
	return c.do(http.MethodGet, path, nil, out, raw)
}

func (c *client) post(path string, body, out any, raw bool) error {
	return c.do(http.MethodPost, path, body, out, raw)
}

func (c *client) delete(path string) error {
	return c.do(http.MethodDelete, path, nil, nil, false)
}

func (c *client) do(method, path string, body, out any, raw bool) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, "http://corenet"+path, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("corenetd is not reachable on %s: %w", c.socket, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var apiErr protocol.ErrorResponse
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("%s", apiErr.Error.Message)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if raw {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
