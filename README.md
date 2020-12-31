# netutil
[![PkgGoDev](https://pkg.go.dev/badge/github.com/wmark/go.netutil)](https://pkg.go.dev/github.com/wmark/go.netutil)

## IdleTracker: Close Idle Server Instances

When the `http.Server` you'd configure this with has not received new connections
for some time, the context `IdleTracker` implements will be *done*, which can be used to
initiate an orderly *shutdown*.

Works best with **systemd's** *socket-activated services**.

## AcceptedConnection

Remember **inetd** or **xinetd**? **Systemd** can start server instances for every
incoming connection, which `AcceptedConnection` upgrades to a `net.Listener` that you
can pass to `http.Server`.

For whenever every process needs its own ephemeral environment, or any isolation
from other instances. Like *code runners*, such as found in CI or *Godoc's “playground”*.
