# CoreNet Specifications

## 1. Overview

CoreNet is an independent network built around existing Internet and web technologies.

CoreNet provides:

* Network connectivity
* Node identity
* Domain naming
* Service discovery
* Domain records
* Peer communication
* Search
* Optional application services

CoreNet does not define how applications must be implemented.

A CoreNet service may be written in:

```text
Go
Python
PHP
Rust
C
C++
Java
JavaScript
or any other language
```

If a service implements the required protocol, it can operate on CoreNet.

---

# 2. Web Compatibility

CoreNet should reuse existing technologies wherever practical.

CoreNet services may use:

```text
HTTP
HTTPS
HTML
CSS
JavaScript
DNS
TCP/IP
TLS
```

CoreNet must not require a custom browser.

A normal web browser should be able to access CoreNet web services.

Example:

```text
https://lord.core
https://wiki.core
https://search.core
```

---

# 3. Domain Namespace

CoreNet uses the `.core` namespace.

Examples:

```text
search.core
register.core
email.core
wiki.core
lord.core
```

Domains identify services rather than programming languages or implementations.

A domain may point to a service implemented in PHP, Go, Python, Rust or another language.

---

# 4. Nodes

A CoreNet node is a machine or environment participating in the network.

A node may provide:

* Web services
* DNS
* Search
* Registry services
* Federation
* P2P services
* Other CoreNet applications

A node runs:

```text
corenetd
```

The daemon handles the CoreNet control plane.

---

# 5. CLI

The `corenet` command provides the user-facing interface.

Initial commands:

```text
corenet connect
corenet disconnect
corenet status

corenet identity create
corenet identity show

corenet domain register
corenet domain info
corenet domain list

corenet node list
corenet node info

corenet peer add
corenet peer list
corenet peer info

corenet search
```

The CLI communicates with the local `corenetd` daemon.

A Unix socket may be used:

```text
/run/corenet/corenetd.sock
```

---

# 6. Identity

CoreNet identities use Ed25519 public/private key pairs.

Each node has an identity.

Example:

```text
CoreNet ID:
core1-8f7a2c91
```

Private keys are stored locally.

Example:

```text
~/.corenet/
├── identity.key
└── identity.pub
```

The private key must never be transmitted to another node.

The public key may be distributed through CoreNet records.

---

# 7. Domain Ownership

A domain is associated with a CoreNet identity.

Example:

```json
{
  "version": 1,
  "domain": "lord.core",
  "owner": "core1-8f7a2c91",
  "public_key": "...",
  "service": "http",
  "address": "10.42.0.10",
  "port": 80,
  "timestamp": 1788710000,
  "signature": "..."
}
```

The owner signs the domain record using its private key.

Other nodes can verify the record using the associated public key.

---

# 8. Domain Records

A domain record describes where a service can be reached.

Minimum fields:

```text
version
domain
owner
public_key
service
address
port
timestamp
signature
```

Additional fields may be introduced in future protocol versions.

Unknown fields should be ignored by implementations unless explicitly marked as required.

---

# 9. Service Discovery

CoreNet nodes may advertise services.

Docker is one supported discovery mechanism.

Example:

```yaml
labels:
  corenet.name: wiki.core
  corenet.service: http
  corenet.port: "80"
```

The resulting service may be published as:

```text
wiki.core → HTTP → port 80
```

CoreNet does not require Docker.

Other implementations may use:

* Static configuration
* Kubernetes
* systemd
* Bare metal
* Virtual machines
* Custom service managers

---

# 10. HTTP Services

HTTP is the primary application protocol.

A CoreNet HTTP service behaves like an ordinary web server.

Example:

```text
GET / HTTP/1.1
Host: wiki.core
```

The application can return ordinary HTML.

CoreNet does not require a special HTML format.

---

# 11. Search

`search.core` is the default CoreNet search service.

The initial search engine uses:

```text
Go
SQLite
SQLite FTS5
```

The crawler may be implemented separately in Python.

The crawler retrieves ordinary HTML documents, extracts:

* URL
* Title
* Text
* Links

and submits them to the search engine.

Example:

```http
POST /api/index
Content-Type: application/json
```

```json
{
  "url": "https://example.core/",
  "title": "Example CoreNet Site",
  "content": "Welcome to CoreNet..."
}
```

The search frontend should initially use only:

```text
HTML
CSS
```

JavaScript is not required.

---

# 12. Crawler

The CoreNet crawler is responsible for discovering web content.

Basic process:

```text
Fetch
  ↓
Parse HTML
  ↓
Extract title
  ↓
Extract text
  ↓
Extract links
  ↓
Index document
  ↓
Queue new links
```

The crawler should remain deliberately simple during the first implementation.

It should not require a distributed crawling system.

---

# 13. Search Interface

The default search page is:

```text
search.core
```

The interface should prioritize:

* Fast loading
* Plain HTML
* Simple CSS
* Accessibility
* No JavaScript requirement
* Minimal dependencies

Example:

```text
                    Search!

        [________________________]

             [ Search ]
       [ I'm Feeling Lucky ]


       search | email | domains | docs


       add site | pages indexed
```

The visual design intentionally takes inspiration from the early web.

---

# 14. Federation

A CoreNet peer is another independent CoreNet installation.

Example:

```text
CoreNet A
    │
    │
    ▼
CoreNet B
```

Peer metadata should contain:

```text
CoreNet ID
public key
endpoint
protocol version
capabilities
```

Peers exchange signed network information.

Federation should not require a single central authority.

---

# 15. P2P

CoreNet may use a distributed hash table for decentralized discovery.

The DHT may store information such as:

```text
domain → owner
domain → service
node → endpoint
node → capabilities
```

The exact DHT protocol is implementation-defined until the specification is finalized.

---

# 16. TLS

CoreNet uses standard TLS.

CoreNet must not implement its own encryption algorithm.

The intended web interface is:

```text
https://example.core
```

CoreNet identity may be used to establish domain ownership and associate certificates with domains.

Browser trust remains a separate concern from network identity.

---

# 17. email.core

`email.core` is the first CoreNet mail service.

The initial implementation is intentionally simple.

Minimum functionality:

```text
Send
Inbox
Read
Delete
```

Example API:

```text
POST /api/send
GET  /api/inbox
GET  /api/message/:id
POST /api/delete/:id
```

The first version does not require SMTP or IMAP.

Those protocols may be added later if there is a practical reason to do so.

---

# 18. Versioning

CoreNet protocol versions use:

```text
MAJOR.MINOR
```

Example:

```text
1.0
```

Implementations should advertise their supported protocol version.

Backward-compatible additions should increment the minor version.

Breaking protocol changes require a major version change.

---

# 19. Errors

APIs should use ordinary HTTP status codes where HTTP is used.

Examples:

```text
200 OK
201 Created
400 Bad Request
401 Unauthorized
403 Forbidden
404 Not Found
409 Conflict
429 Too Many Requests
500 Internal Server Error
```

Responses should preferably be simple and machine-readable.

---

# 20. Security Principles

CoreNet should prefer established standards over custom cryptography.

Principles:

```text
Use Ed25519 for identity.
Use TLS for transport encryption.
Never transmit private keys.
Sign ownership records.
Validate remote records.
Do not trust discovered services blindly.
Do not expose privileged control APIs publicly.
```

Docker socket access must be treated as highly privileged.

A CoreNet service must never expose the Docker socket directly to untrusted network clients.

---

# 21. Language Independence

CoreNet does not define an official application language.

The reference implementation may use:

```text
Go
Python
PHP
```

for different components, but this is an implementation choice.

For example:

```text
corenetd       → Go
search engine  → Go
crawler        → Python
register.core  → PHP
email.core     → PHP / Go
website        → any language
```

Interoperability is determined by the protocol, not the programming language.

---

# 22. Simplicity

CoreNet follows a strict principle:

> Do not build infrastructure before it is needed.

The network should prefer:

```text
simple
stable
boring
interoperable
```

over:

```text
complex
distributed
over-engineered
```

A feature should only be introduced when it solves a real problem.

---

# 23. CoreNet Philosophy

CoreNet is not intended to replace the web.

It is intended to provide an alternative network on which the web can continue to exist.

The network provides the infrastructure.

Applications remain independent.

A developer should be able to create:

```text
https://blog.core
https://wiki.core
https://git.core
https://forum.core
https://files.core
```

using whatever technology they prefer.

The only requirement is interoperability with CoreNet's protocols.

**CoreNet owns the network, not the applications.**
