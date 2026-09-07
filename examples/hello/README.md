# hello.core

A minimal CoreNet service: two static files served by any HTTP server.

Serve the directory, then register it with the local node:

```bash
python3 -m http.server 8081 --directory examples/hello
corenet service add hello.core 127.0.0.1 8081
```

With `corenet connect` active, <http://hello.core> now loads this page.

To make the registration permanent, put it in the configuration file instead:

```json
{
  "services": [
    { "name": "hello.core", "address": "127.0.0.1", "port": 8081 }
  ]
}
```
