# Using Gnomock over HTTP

If you use Go, please refer to [Using Gnomock in Go
applications](../README.md#using-gnomock-in-go-applications) section.
Otherwise, you'll need to setup a helper container, and communicate with it
over HTTP.

## Security

`gnomock` is a test tool, not a service to expose. Read this before running
it anywhere other than a local machine or a disposable CI runner:

- **No authentication.** The server listens on all interfaces (`:23042`) and
  serves every request it receives. It has no authentication, authorization or
  TLS.
- **It controls your Docker daemon.** The server runs with the host's Docker
  socket mounted, so whoever can reach its port can start containers on the
  host.
- **Requests can ask for host-level access.** `/start` requests honor the
  `privileged`, `host_mounts`, `customImage`, `cmd`, `entrypoint`, `user` and
  `auth` options (older versions silently ignored most of them). A single
  request can start a privileged container of any image with any host path
  mounted, which is equivalent to root access on the Docker host.

Keep the port private: publish it on the loopback interface only (for example
`-p 127.0.0.1:23042:23042`), don't publish it on shared or public networks,
and don't run the server on hosts whose Docker daemon you need to protect.

## Running the server

### Using Github Actions

For convenience, there is a [Github
Action](https://github.com/marketplace/actions/gnomock) that starts a Gnomock
server on port 23042 in a single step:

```
steps:
  - name: Gnomock
    uses: gnomock/github-action@master

  - name: Test
    run: |
      echo "running tests..."
      # run tests that use Gnomock server on port 23042
```

### Run directly

To start a Gnomock server without using Github Action, run the following on any
Unix-based system:

```bash
docker run --rm \
    -p 23042:23042 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v $PWD:$PWD \
    --privileged \
    orlangure/gnomock
```

`-p 23042:23042` exposes a port on the host to communicate with `gnomock`. You
can use any port you like, just make sure to configure the client properly.
This publishes the port on every host interface; prefer
`-p 127.0.0.1:23042:23042` (see [Security](#security)).

`-v /var/run/docker.sock:/var/run/docker.sock` allows `gnomock` to communicate
with the docker engine running on host. Without it `gnomock` can't access
docker.

`--privileged` may be required on some systems.

To start LocalStack 2026.x containers, pass an auth token either in the
request (`auth_token` in the localstack preset) or to the server itself with
`-e LOCALSTACK_AUTH_TOKEN`; the server uses its own variable when a request
doesn't include a token.

If you use any file-related `gnomock` options, like `WithQueriesFile`, you have
to make the path you use available inside the container:

```
# this makes the current folder appear inside the container under the same
# path and name:
-v `pwd`:`pwd`
```

Any program in any language can communicate with `gnomock` server using OpenAPI
3.0 [specification](https://app.swaggerhub.com/apis/orlangure/gnomock/).

Below is an example of setting up a **MySQL** container using a `POST` request:

```
$ cat mysql-preset.json
{
  "preset": {
    "db": "mydb",
    "user": "gnomock",
    "password": "p@s$w0rD",
    "queries": [
      "create table foo(bar int)",
      "insert into foo(bar) values(1)"
    ],
    "queries_files": ["/home/gnomock/project/testdata/mysql/queries.sql"]
  },
  "options": {}
}

$ curl --data @mysql-preset.json http://127.0.0.1:23042/start/mysql
{
  "id": "f5d08dc84421",
  "host": "127.0.0.1",
  "ports": {
    "default": {
      "protocol": "tcp",
      "port": 35973
    }
  }
}
```

For more details and a full specification, see
[documentation](https://app.swaggerhub.com/apis/orlangure/gnomock/). Use
OpenAPI generator to create API wrappers in the language of your choice.

