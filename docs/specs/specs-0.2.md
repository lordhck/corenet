# CoreNet 0.2 Specification

This document describes CoreNet 0.2: automatic service discovery through Docker.

CoreNet 0.2 builds directly on the network foundation defined by CoreNet 0.1.

CoreNet 0.1 uses configuration files and runtime registrations to discover services.

CoreNet 0.2 adds Docker as an automatic service discovery source.

Identity, domain ownership, federation, DHT, search and TLS remain outside the scope of this version.

---

## 1. Changes from 0.1

CoreNet 0.2 adds:

```text
Docker service discovery
Docker label configuration
Automatic service registration
Automatic service removal
Docker-aware service lifecycle
```

The following 0.1 concepts remain unchanged:

```text
node
corenetd
corenet
.core names
DNS
HTTP proxy
peer listener
control socket
directory
service resolution
```

The existing 0.1 directory remains the source of truth for local service resolution.

Docker is an additional way of populating that directory.

---

## 2. Docker

A CoreNet 0.2 node may use the Docker Engine API to discover services.

Docker is not part of the CoreNet network protocol itself.

It is a local service discovery mechanism.

The recommended Docker API endpoint is:

```text
/var/run/docker.sock
```

The Docker socket is privileged and must never be exposed through CoreNet.

`corenetd` communicates with Docker locally and converts discovered containers into ordinary CoreNet service entries.

The Docker Engine API is ordinary HTTP and JSON over a Unix socket. An
implementation does not need a Docker SDK, and the reference implementation
does not use one.

An implementation must pin the Engine API version it requests, and must use
read-only endpoints only:

```text
GET /v<version>/containers/json
GET /v<version>/events
```

`corenetd` never issues a Docker request that creates, changes, starts or
stops anything.

```text
Docker
   │
   │ Docker API
   ▼
corenetd
   │
   │ service registration
   ▼
CoreNet directory
```

---

## 3. Docker Labels

A Docker container becomes a CoreNet service by providing CoreNet labels.

The minimum configuration is:

```yaml
labels:
  corenet.name: wiki.core
  corenet.service: http
  corenet.port: "80"
```

The labels are:

```text
corenet.name
corenet.service
corenet.port
```

---

## 4. `corenet.name`

`corenet.name` specifies the `.core` name advertised by the container.

Example:

```text
corenet.name=wiki.core
```

The name follows the same normalization and validation rules defined by CoreNet 0.1.

Names must be:

```text
lowercase
without a trailing dot
valid DNS labels
inside .core
```

If the name is invalid, the container is ignored.

---

## 5. `corenet.service`

`corenet.service` specifies the type of service.

CoreNet 0.2 defines:

```text
http
```

as the only service type.

Example:

```text
corenet.service=http
```

The value is informational for the service directory and allows future CoreNet versions to introduce additional service types.

A container whose `corenet.service` is not `http` is ignored, and the reason is logged.

A container advertises exactly one CoreNet name. Serving several names from one container is outside CoreNet 0.2.

---

## 6. `corenet.port`

`corenet.port` specifies the TCP port of the service.

Example:

```text
corenet.port=80
```

The value must be an integer between:

```text
1-65535
```

The port refers to the port reachable by `corenetd`.

It is not necessarily the host port published by Docker.

---

## 7. Example

A simple Docker Compose service:

```yaml
services:
  wiki:
    image: nginx:alpine

    labels:
      corenet.name: wiki.core
      corenet.service: http
      corenet.port: "80"
```

When the container is running, `corenetd` discovers:

```text
wiki.core
    service: http
    port: 80
```

The service becomes part of the local CoreNet directory.

---

## 8. Container Address

The service address used by CoreNet must be reachable from `corenetd`.

For a Docker service, `corenetd` should prefer the container's address on a suitable Docker network.

Example:

```text
wiki container
      │
      ▼
10.42.0.10:80
```

The resulting CoreNet service entry becomes conceptually:

```json
{
  "name": "wiki.core",
  "address": "10.42.0.10",
  "port": 80,
  "source": "docker"
}
```

The exact Docker network selected by an implementation is local configuration and is not part of the CoreNet protocol.

---

## 9. Discovery

`corenetd` periodically inspects running Docker containers.

The process is:

```text
list containers
      ↓
inspect labels
      ↓
validate labels
      ↓
determine address
      ↓
update directory
```

Containers without `corenet.name` are ignored.

Containers without `corenet.service` are ignored.

Containers without `corenet.port` are ignored.

Invalid services must not cause `corenetd` to terminate.

---

## 10. Service Lifecycle

Docker services are tied to the lifecycle of their containers.

When a valid container starts:

```text
container starts
      ↓
corenetd discovers container
      ↓
service added
```

When a container stops or is removed:

```text
container stops
      ↓
corenetd detects change
      ↓
service removed
```

A Docker-discovered service must not remain in the directory after its container has disappeared.

---

## 11. Service Source

CoreNet 0.1 already defines the `source` field.

CoreNet 0.2 adds:

```text
docker
```

as a valid local source.

Example:

```json
{
  "name": "wiki.core",
  "address": "10.42.0.10",
  "port": 80,
  "source": "docker"
}
```

The existing sources remain:

```text
config
runtime
<node-id>
docker
```

---

## 12. Service Precedence

CoreNet 0.1 defines service precedence:

```text
runtime registration
        beats
configuration
        beats
remote node
```

CoreNet 0.2 adds Docker services.

The local precedence becomes:

```text
runtime
   ↓
config
   ↓
docker
   ↓
remote
```

Therefore:

```text
runtime > config > docker > remote
```

A Docker-discovered service must not silently replace a locally configured or runtime-registered service with the same name.

Precedence is an override, not an error. A name provided by Docker may be
taken over by configuration or by a runtime registration:

```text
POST /v1/services   wiki.core   ->  201 Created
```

succeeds even when Docker already provides `wiki.core`. The Docker service
remains in the node's directory, shadowed, and resolution returns to it when
the runtime registration is removed:

```text
DELETE /v1/services/wiki.core   ->  204 No Content
wiki.core                       ->  the Docker service again
```

`409 conflict` is returned only when the name is already provided by
configuration or by another runtime registration. Shadowing is reported in
the node's log, so an operator can see that a Docker service is not being
routed.

---

## 13. Duplicate Docker Names

Two Docker containers on the same node must not provide the same CoreNet name.

Example:

```text
container A → wiki.core
container B → wiki.core
```

This is a conflict.

`corenetd` must not silently select one container.

The affected name is marked as conflicted. A conflicted name is not routed,
and it behaves as follows:

```text
DNS                     NXDOMAIN
HTTP proxy              409, plain HTML, naming the conflict
GET /v1/resolve         409 conflict
GET /v1/services        not listed as a service; listed under conflicts
peer /v1/services       not advertised
```

A conflicted name is never advertised to another node: a name this node
cannot route is not a name it can offer.

The conflict is logged when it appears and when it clears.

A conflict exists only within the Docker source. If configuration or a
runtime registration provides the same name, that service is routed normally
under the precedence rules of section 12, and the Docker conflict is still
reported.

When one of the conflicting containers disappears, the remaining container's
service becomes routable again without operator action.

---

## 14. Existing Directory API

CoreNet 0.2 does not replace the 0.1 directory API.

The existing endpoints remain:

```text
GET    /v1/status
GET    /v1/services
POST   /v1/services
DELETE /v1/services/{name}
GET    /v1/resolve?name=<name>
GET    /v1/nodes
GET    /v1/nodes/{id}
```

Docker-discovered services appear in:

```text
GET /v1/services
```

and can be resolved through:

```text
GET /v1/resolve?name=<name>
```

On the control socket, `GET /v1/services` additionally reports the names that
are known but not routable:

```json
{
  "services": [
    { "name": "wiki.core", "address": "10.42.0.10", "port": 80, "source": "docker" }
  ],
  "conflicts": [
    { "name": "blog.core", "reason": "two Docker containers provide this name" }
  ]
}
```

The field is additive: a CoreNet 0.1 client ignores it, as required by
specifications section 8. The peer listener never includes it.

Docker does not introduce a public Docker API through `corenetd`.

---

## 15. Docker Services Are Not Runtime Registrations

Docker-discovered services are managed by `corenetd`.

They must not be treated as runtime registrations.

Therefore:

```bash
corenet service remove wiki.core
```

must not remove a service whose source is:

```text
docker
```

The service disappears when the corresponding Docker container disappears or its labels become invalid.

---

## 16. Peer Discovery

CoreNet 0.2 does not change the peer protocol from 0.1.

A node still advertises only its own local services.

Docker-discovered services are therefore advertised to configured peers in exactly the same way as other local services.

```text
Docker
   ↓
corenetd
   ↓
local directory
   ↓
peer /v1/services
   ↓
remote corenetd
```

A node never relays Docker services learned from another node.

---

## 17. HTTP

The CoreNet 0.1 HTTP proxy remains unchanged.

A browser requests:

```http
GET / HTTP/1.1
Host: wiki.core
```

`corenetd` performs the existing directory lookup:

```text
wiki.core
    ↓
10.42.0.10:80
    ↓
HTTP backend
```

Docker does not change the HTTP protocol.

A Docker container hosting a CoreNet website is simply another HTTP service.

---

## 18. DNS

The CoreNet 0.1 DNS behavior remains unchanged.

Known `.core` names resolve to the local CoreNet HTTP listener.

```text
wiki.core
    ↓
local corenetd HTTP listener
    ↓
directory lookup
    ↓
Docker service
```

Docker addresses are never exposed directly through CoreNet DNS.

---

## 19. Configuration

Docker discovery is configured in a `docker` block:

```json
{
  "docker": {
    "enabled": true,
    "socket": "/var/run/docker.sock",
    "network": "",
    "poll_interval_seconds": 5
  }
}
```

```text
enabled                 use Docker as a discovery source
socket                  path to the Docker API socket
network                 preferred Docker network name, empty means automatic
poll_interval_seconds   how often containers are re-read
```

If the Docker configuration is absent, implementations should use:

```text
enabled: true
```

when Docker is available.

Enabling Docker discovery by default means that any container on the host may
claim a `.core` name by setting a label. That is the intended behaviour on a
machine with one operator. On a host where containers are not all trusted,
`enabled: false` and explicit configuration are the correct choice.

If Docker is unavailable, `corenetd` continues operating normally using the other service sources.

Docker availability must not prevent a node from starting.

---

## 20. Docker Network Selection

A CoreNet implementation may run multiple Docker networks.

The implementation must select a network from which the service address is reachable by `corenetd`.

Selection is deterministic:

```text
1. the network named by docker.network, if it is configured
2. the container's only network, if it has exactly one
3. otherwise the networks sorted by name, first one with an address
```

If no suitable address can be determined, the container is not registered.

An implementation should log the reason.

Example:

```text
docker: wiki.core ... no reachable address ... ignored
```

---

## 21. Updates

Changes to Docker labels must be reflected in the CoreNet directory.

For example:

```text
wiki.core
    ↓
blog.core
```

If the container changes:

```text
corenet.name=wiki.core
```

to:

```text
corenet.name=blog.core
```

the directory must eventually become:

```text
blog.core
```

and:

```text
wiki.core
```

must be removed.

---

## 22. Polling and Events

An implementation may use either:

```text
Docker events
```

or:

```text
periodic polling
```

to detect container changes.

The mechanism is an implementation detail.

The externally observable result must be the same:

```text
running valid container → service exists
stopped/removed container → service does not exist
```

---

## 23. Failure Handling

Docker failures must not terminate `corenetd`.

For example:

```text
Docker unavailable
Docker socket unavailable
Docker API error
invalid container metadata
unreachable container address
```

The node continues serving:

```text
configured services
runtime services
remote services
```

Docker discovery may resume automatically when Docker becomes available again.

---

## 24. Security

The Docker socket provides extensive control over the Docker host.

Therefore:

* The Docker socket must never be exposed to CoreNet peers.
* CoreNet APIs must never proxy arbitrary Docker API requests.
* Docker-provided values must be treated as untrusted input.
* Container labels must be validated.
* Container addresses must be validated.
* Ports must be validated.
* No label value may be executed as a command.

CoreNet 0.2 introduces no authentication for Docker discovery.

The Docker API is considered trusted only because it is accessed locally.

---

## 25. Backwards Compatibility

A CoreNet 0.2 node must continue to understand CoreNet 0.1 service entries.

A CoreNet 0.1 node may connect to a CoreNet 0.2 node.

Docker-specific behavior is local to the 0.2 node.

A 0.1 peer does not need to understand Docker.

From the peer's perspective, a Docker service is simply a local service advertised through:

```text
GET /v1/services
```

A receiving node replaces the `source` of everything it learns with the ID of
the node it learned it from, exactly as in CoreNet 0.1. The value `docker`
therefore never appears as the source of a remote service.

---

## 26. Non-Goals

The following are explicitly outside CoreNet 0.2:

```text
Cryptographic identity
Domain ownership
Signed records
Federation
DHT
Global service discovery
Search
Crawler
TLS
HTTPS
Email
User accounts
Docker orchestration
Kubernetes integration
```

These belong to later versions or are outside the CoreNet protocol entirely.

---

## 27. Reference Flow

A complete CoreNet 0.2 flow is:

```text
Docker container starts
        │
        ▼
CoreNet labels are read
        │
        ▼
corenetd validates labels
        │
        ▼
Container address is determined
        │
        ▼
Service enters local directory
        │
        ├───────────────┐
        ▼               ▼
   DNS resolution   Peer directory
        │
        ▼
    HTTP proxy
        │
        ▼
   Docker service
```

The goal of CoreNet 0.2 is to make service deployment automatic without changing the simple network model established in CoreNet 0.1.
