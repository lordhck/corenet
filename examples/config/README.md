# Example configurations

`node.json` is a normal CoreNet node. It uses the privileged ports, so
`corenetd` needs root or `cap_net_bind_service`, and browsers can reach
`http://hello.core` without a port number. Install it as
`/etc/corenet/config.json`.

`dev.json` runs everything on unprivileged ports for development. Names still
resolve, but the proxy lives on port 8080, so pages load from
`http://hello.core:8080`:

```bash
corenetd --config examples/config/dev.json
corenet --socket /tmp/corenet/corenetd.sock status
```

Docker discovery is on by default wherever `/var/run/docker.sock` exists, and
needs no configuration. To point it at another socket, prefer one Docker
network, slow the polling down or turn it off entirely:

```json
{
  "docker": {
    "enabled": true,
    "socket": "/var/run/docker.sock",
    "network": "corenet",
    "poll_interval_seconds": 5
  }
}
```

A node without Docker, or without permission to read the socket, logs the
reason once and keeps serving its other services.

Two nodes on one machine: copy `dev.json`, change `node.id`, every port and
the control socket path, and list the other node's `peer` address in `nodes`.
