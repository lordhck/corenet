# CoreNet 0.1 Specification

This document describes what CoreNet 0.1 is: the network foundation, and
nothing more. Identity, domain ownership, Docker discovery, search,
federation, DHT and TLS belong to later versions and are deliberately absent.

1. Network
2. Node
3. corenetd
4. corenet CLI
5. Node addressing
6. DNS
7. HTTP
8. Service discovery
9. Directory API
10. Resolve API
11. Error responses
12. Protocol version

---

## 1. Network

A CoreNet 0.1 network is one or more nodes that know about each other because
somebody wrote their addresses in a configuration file.

```text
node-a ──── node-b
```

Each node keeps a directory of `.core` names and the services behind them. A
node learns names from three places:

```text
its configuration file
runtime registrations made through the CLI or the API
the directories of the nodes it was configured to know
```

There is no discovery protocol, no gossip and no central registry. A node that
is not configured on either side is not part of the network.

---

## 2. Node

A node is a machine running `corenetd`. It has:

```text
a node ID          identifies the node inside the network
a node name        human-readable label, optional
a directory        the .core names it can resolve
```

The node ID is a plain string. Because 0.1 has no identities and no
signatures, the node ID is a label, not a claim: a node states its ID, and the
nodes configured to talk to it believe it. Cryptographic identity arrives in
0.3.

Every service a node serves itself is local. Every service it learned from
another node is remote and is attributed to that node.

---

## 3. corenetd

`corenetd` is the node daemon. It runs four listeners:

```text
dns       .core name resolution                  UDP + TCP
http      reverse proxy for CoreNet services     TCP
peer      read-only directory for other nodes    TCP
control   the CLI's API                          Unix socket
```

Any of `dns`, `http` and `peer` may be disabled by configuring an empty
address. The control socket is always present.

Configuration is a JSON file, searched in this order:

```text
--config <path>
/etc/corenet/config.json
~/.corenet/config.json
built-in defaults
```

```json
{
  "node": { "id": "node-a", "name": "laptop" },
  "listen": {
    "dns": "127.0.0.1:53",
    "http": "127.0.0.1:80",
    "peer": "0.0.0.0:7000",
    "control": "/run/corenet/corenetd.sock"
  },
  "services": [
    { "name": "hello.core", "address": "127.0.0.1", "port": 8081 }
  ],
  "nodes": ["192.168.1.20:7000"],
  "pull_interval_seconds": 30
}
```

Unknown fields are ignored, as required by specifications section 8.

`SIGHUP` re-reads the file and applies the new services and nodes. Listen
addresses, the node ID and the pull interval are applied on restart only.

Ports 53 and 80 require root or `cap_net_bind_service`. A node may run
entirely on unprivileged ports, at the cost of a port number in the URL.

---

## 4. corenet CLI

`corenet` speaks to the local `corenetd` over the control socket. It never
talks to the network itself.

```text
corenet connect                              resolve .core names on this machine
corenet disconnect                           stop resolving .core names
corenet status                               show the local node

corenet service list                         list every resolvable service
corenet service add <name> <address> <port>  register a service
corenet service remove <name>                remove a registered service

corenet resolve <name>                       look up a .core name
corenet node list                            list this node and its peers
corenet node info <id>                       show one node
```

Options: `--config <path>`, `--socket <path>`, `--json`.

`connect` and `disconnect` are the only commands that change the system. They
require root, they are idempotent, and they touch exactly one file:

```text
/etc/systemd/resolved.conf.d/corenet.conf
```

```ini
[Resolve]
DNS=127.0.0.1
Domains=~core
```

`Domains=~core` routes only the `.core` domain to `corenetd`. Ordinary
Internet resolution is untouched. Where systemd-resolved is not running,
`connect` prints the configuration instead of guessing at another resolver.

---

## 5. Node addressing

A node is addressed by the `host:port` of its peer listener.

```text
192.168.1.20:7000
```

That address is what appears in another node's `nodes` list. Node IDs are
learned by asking the node, not by configuration.

Services are addressed by the `address:port` of the server behind them. The
address is reached from the node that serves the service, not from the browser
— a browser never connects to a service directly.

---

## 6. DNS

`corenetd` answers DNS queries for the `.core` namespace, and only for it.

```text
A      known name        -> the node's HTTP listener address, TTL 0
A      unknown .core     -> NXDOMAIN
AAAA   known name        -> NOERROR, no answer
other  known name        -> NOERROR, no answer
any    outside .core     -> REFUSED
```

Every known name resolves to the same address: the local HTTP proxy. A
browser therefore always connects to its own node, whether the service runs
locally or on another node. Answers are authoritative, are never cached
(TTL 0), and no query is ever forwarded. `corenetd` is not a resolver.

---

## 7. HTTP

HTTP is the application protocol of CoreNet 0.1. There is no HTTPS: TLS is
0.7.

The proxy routes on the `Host` header alone:

```http
GET / HTTP/1.1
Host: wiki.core
```

```text
Host -> directory lookup -> service address -> proxied request
```

The `Host` header is preserved, so a backend may serve several CoreNet names.
A service is an ordinary HTTP server; CoreNet requires nothing of it.

```text
unknown name          404, plain HTML
backend unreachable   502, plain HTML
```

---

## 8. Service discovery

A service becomes resolvable in one of two ways.

Statically, in the configuration file:

```json
{ "name": "wiki.core", "address": "127.0.0.1", "port": 8080 }
```

Or at runtime, through the CLI or the directory API:

```bash
corenet service add wiki.core 127.0.0.1 8080
```

Runtime registrations live in memory and are lost when the node restarts.
Configured services are owned by the file and cannot be removed through the
API.

Names must be normalized: lowercase, no trailing dot, ordinary DNS labels, and
inside `.core`.

Between nodes, discovery is a pull. Every `pull_interval_seconds`, a node asks
each configured peer for its identity and its local directory:

```text
GET /v1/info
GET /v1/services
```

What it learns replaces everything it previously learned from that node. A
node that cannot be reached has its services withdrawn immediately, so an
unreachable node never leaves stale names behind.

Precedence when a name exists more than once:

```text
runtime registration   beats   configuration file
configuration file     beats   any remote node
lowest node ID         beats   any other remote node
```

A node advertises only its own local services. It never relays what it learned
from a third node, so a directory cannot loop.

---

## 9. Directory API

Two listeners serve HTTP APIs. The control socket is local and may write:

```text
GET    /v1/status
GET    /v1/services
POST   /v1/services            {"name","address","port"}   201
DELETE /v1/services/{name}                                 204
GET    /v1/resolve?name=<name>
GET    /v1/nodes
GET    /v1/nodes/{id}
```

The peer listener is on the network, is read-only, and advertises only local
services:

```text
GET /v1/info
GET /v1/services
GET /v1/resolve?name=<name>
```

There is no authentication in 0.1. The peer listener must only be exposed to a
trusted network, and the control socket is restricted to its owner.

```json
{
  "services": [
    { "name": "wiki.core", "address": "127.0.0.1", "port": 8080, "source": "config" }
  ]
}
```

`source` is `config`, `runtime`, or the ID of the node the service was learned
from.

---

## 10. Resolve API

```http
GET /v1/resolve?name=wiki.core
```

```json
{
  "name": "wiki.core",
  "address": "10.0.0.2",
  "port": 8080,
  "source": "node-b"
}
```

```text
200   the name resolves
400   the name is not a valid .core name
404   the name is not in this node's directory
```

Resolution is a directory lookup and nothing else. It performs no network
request, and it does not check whether the service is actually up.

---

## 11. Error responses

APIs use ordinary HTTP status codes with a small JSON body:

```json
{ "error": { "code": "not_found", "message": "unknown service: wiki.core" } }
```

```text
400   bad_request    malformed input, or an invalid name
404   not_found      no such service, node or endpoint
409   conflict       the name is already served by this node
502   bad_gateway    the service did not answer
500   internal       the node failed
```

The HTTP proxy answers a browser with plain HTML instead of JSON, since its
client is a person.

---

## 12. Protocol version

CoreNet 0.1 implements protocol version `0.1`. It is reported by:

```text
GET /v1/status    on the control socket
GET /v1/info      on the peer listener
corenet version
corenetd --version
```

A node does not refuse to talk to a node reporting another version. Version
negotiation is not needed while there is only one version, and inventing it
now would be the kind of infrastructure specifications section 22 tells us not
to build.
