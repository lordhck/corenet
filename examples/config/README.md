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

Two nodes on one machine: copy `dev.json`, change `node.id`, every port and
the control socket path, and list the other node's `peer` address in `nodes`.
