# The Core Network

CoreNet is a small, open-source alternative network built around existing web technologies.

The goal is simple: build an independent network where people can host websites and services using technologies they already know.

```text
HTTP
HTTPS
HTML
CSS
DNS
TCP/IP
```

Services can be written in any language:

```text
Go
Python
PHP
Rust
C
...
```

## The idea

Connect to CoreNet:

```bash
corenet connect
```

Then access CoreNet domains using a normal web browser:

```text
https://search.core
https://docs.core
https://domains.core
```

CoreNet provides the network infrastructure.

Applications remain independent.

> CoreNet owns the network, not the applications.

## Roadmap

```text
- [x] 0.1  Network foundation
- [x] 0.2  Docker + service discovery
- [] 0.3  Identity + domains + DNS
- [] 0.4  Search + crawler
- [] 0.5  Federation
- [] 0.6  P2P / DHT
- [] 0.7  HTTPS / TLS
- [] 0.8  email.core
- [] 0.9  Public testnet
- [] 1.0  CoreNet stable
```

## Philosophy

Keep it simple.

```text
HTML first
CSS only by default
JavaScript optional
Existing standards
Language agnostic
No unnecessary complexity
```

CoreNet is inspired by the simplicity of the early web.

## Try it

CoreNet needs Go, and Docker only if you want container discovery. It runs on
unprivileged ports without touching a single system file:

```bash
make build          # or: go build -o corenetd ./cmd/corenetd

python3 -m http.server 8081 --directory examples/hello &
./corenetd --config examples/config/dev.json &

./corenet --socket /tmp/corenet/corenetd.sock service list
curl -H 'Host: hello.core' http://127.0.0.1:8080/
```

Containers become services on their own, with three labels and no CoreNet
command at all:

```bash
docker run -d --label corenet.name=wiki.core \
  --label corenet.service=http --label corenet.port=80 nginx:alpine

./corenet --socket /tmp/corenet/corenetd.sock service list
curl -H 'Host: wiki.core' http://127.0.0.1:8080/
```

The same three labels work in `compose.yaml`. Stop the container and the name
goes with it.

To reach `http://hello.core` from a browser, install
[`examples/config/node.json`](examples/config/node.json) as
`/etc/corenet/config.json`, run `corenetd` as root, and point this machine's
resolver at it:

```bash
sudo corenet connect
```

`connect` writes one file, `/etc/systemd/resolved.conf.d/corenet.conf`, which
routes only the `.core` domain to the local daemon. `sudo corenet disconnect`
removes it.

End-to-end check, including a two-node network and Docker:

```bash
make check          # vet, race tests, then the end-to-end smoke test
```

`make help` lists the rest. Nothing in CoreNet depends on Make; the targets are
thin wrappers around `go` and `test/smoke.sh`.

## Status

**Pre-alpha**

0.1 and 0.2 are implemented: the `corenet` CLI, the `corenetd` daemon, `.core`
resolution, HTTP services, a local directory, directory exchange between
statically configured nodes, and automatic service discovery from Docker
labels. There is no TLS, no identity and no domain ownership yet — `https://`
arrives in 0.7, identity in 0.3.

See:

* [`docs/specs/specs-0.1.md`](docs/specs/specs-0.1.md) — the network foundation
* [`docs/specs/specs-0.2.md`](docs/specs/specs-0.2.md) — Docker discovery
* [`docs/roadmap.md`](docs/roadmap.md)
* [`docs/specifications.md`](docs/specifications.md)

## License

MIT
