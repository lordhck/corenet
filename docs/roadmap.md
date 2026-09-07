# CoreNet Roadmap

CoreNet is a small, open-source alternative network built around existing web technologies.

The goal is not to reinvent the web. CoreNet provides the network, naming, discovery, identity and services needed to operate an independent network while keeping applications simple.

CoreNet services should be able to use ordinary technologies such as:

* HTTP
* HTTPS
* HTML
* CSS
* DNS
* TCP/IP
* SQLite

Applications may be written in any programming language.

---

## 0.1 — Network Foundation

Build the first working CoreNet network.

### Goals

* Create the `corenet` CLI.
* Create the `corenetd` daemon.
* Establish a local CoreNet network.
* Provide basic node discovery.
* Provide basic HTTP access.
* Provide `.core` name resolution.
* Allow manually configured nodes and services.

### Initial components

```text
corenet
corenetd
dns
```

### Example

```text
corenet connect

https://example.core
```

At this stage, the network can be simple and mostly local.

---

## 0.2 — Docker & Service Discovery

Make CoreNet practical to run with containers.

### Goals

* Integrate with the Docker API.
* Discover CoreNet services automatically.
* Support service labels.
* Automatically register HTTP services.
* Provide a local service directory.

### Example

```yaml
labels:
  corenet.name: wiki.core
  corenet.service: http
  corenet.port: "80"
```

CoreNet should discover the container and make it available as:

```text
https://wiki.core
```

The Docker integration should remain an implementation detail of the node.

---

## 0.3 — Identity, Domains & DNS

Introduce cryptographic identity and domain ownership.

### Goals

* Generate CoreNet identities.
* Use Ed25519 keys.
* Register `.core` domains.
* Sign domain records.
* Resolve domains dynamically.
* Provide domain management through `corenet`.

### CLI

```text
corenet identity create
corenet identity show

corenet domain register example.core
corenet domain info example.core
corenet domain list
```

Private keys must never leave the local machine.

---

## 0.4 — Search

Build the first CoreNet search engine.

The default CoreNet start page is:

```text
search.core
```

### Goals

* Build a simple HTML/CSS search interface.
* Build the search engine in Go.
* Build the crawler in Python.
* Store the initial index in SQLite.
* Use SQLite FTS5 for searching.
* Index ordinary HTML pages.
* Follow links between `.core` sites.
* Provide a simple indexing API.

### Design principles

```text
HTML first
CSS only
JavaScript optional
No frontend frameworks
No unnecessary dependencies
Server-side rendering preferred
```

The search engine should feel like the early web rather than a modern JavaScript-heavy search application.

---

## 0.5 — Federation

Allow independent CoreNet networks to communicate.

```text
CoreNet A
    │
    │ federation
    │
CoreNet B
```

### Goals

* Introduce peer identities.
* Add remote peers.
* Exchange signed records.
* Discover remote services.
* Support multiple independent CoreNet installations.

### CLI

```text
corenet peer add
corenet peer list
corenet peer info
```

A CoreNet installation should not need to know about every node in the entire network.

---

## 0.6 — P2P & DHT

Move CoreNet toward a genuinely distributed network.

### Goals

* Distributed domain lookup.
* Distributed service discovery.
* DHT-based records.
* Reduce dependency on centralized registries.
* Improve peer discovery.

The exact DHT implementation is intentionally left open until the earlier versions have proven what CoreNet actually needs.

---

## 0.7 — HTTPS & TLS

Add secure web connections.

CoreNet should use standard TLS rather than inventing a new encryption protocol.

The target is:

```text
https://lord.core
```

### Goals

* TLS support.
* Certificate association with CoreNet identities.
* Domain ownership verification.
* Secure service-to-service communication.

The relationship between CoreNet identity and browser certificate trust must be defined before this version is considered complete.

---

## 0.8 — email.core

Introduce a simple CoreNet mail service.

The first implementation should intentionally remain small.

### Goals

* Web-based inbox.
* Send messages.
* Read messages.
* Delete messages.
* SQLite storage.
* Simple HTTP API.

Example:

```text
email.core
```

Initial API:

```text
POST /api/send
GET  /api/inbox
GET  /api/message/:id
POST /api/delete/:id
```

SMTP, IMAP and mail federation are not required for the initial implementation.

---

## 0.9 — Public Testnet

Open CoreNet to external users.

### Goals

* Publish Docker images.
* Publish installation documentation.
* Allow independent nodes to join.
* Test federation.
* Test P2P discovery.
* Test NAT traversal.
* Test security.
* Test abuse handling.
* Document operational requirements.

The testnet should be considered experimental.

---

## 1.0 — CoreNet Stable

CoreNet 1.0 represents the first stable network specification.

### Core features

```text
✓ CoreNet nodes
✓ corenet CLI
✓ corenetd daemon
✓ .core domains
✓ Cryptographic identities
✓ Service discovery
✓ Search
✓ Federation
✓ P2P/DHT
✓ HTTPS
✓ email.core
```

### 1.0 principles

CoreNet should remain:

* Open source
* Language agnostic
* Simple
* Interoperable
* Decentralized
* HTTP compatible
* Easy to self-host

CoreNet owns the network.

It does not own the applications running on it.
