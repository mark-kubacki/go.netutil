# IdleTracker: Close Idle Server Instances

[![PkgGoDev](https://pkg.go.dev/badge/github.com/wmark/go.netutil)](https://pkg.go.dev/github.com/wmark/go.netutil)

Once the `http.Server` you'd typically use this in has not received new connections
for some time, the context `IdleTracker` implements is *done* and can be used to
initiate an orderly *shutdown*.

Works best with **systemd's** *socket-activated services**.

## Example

An excerpt from my extended *godoc*:

```golang
import (
	"github.com/coreos/go-systemd/v22/activation"
	netutil "github.com/wmark/go.netutil"
)

var (
	httpAddr = flag.String("http", "[::1]:8080", "HTTP service address")
)

func main() {
	flag.Parse()

	ctx, cancelFn := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:      handler, // http.NewServeMux()
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	var ln net.Listener
	if _, socketActivated := os.LookupEnv("LISTEN_FDS"); socketActivated {
		listeners, err := activation.Listeners()
		if len(listeners) < 1 || err != nil {
			log.Fatalf("Socket activated, but without any listener. Err: %v", err)
		}
		ln = listeners[0]

		lingerCtx := netutil.NewIdleTracker(ctx, 15*time.Minute)
		go func() {
			<-lingerCtx.Done():
			tearDownCtx, _ := context.WithTimeout(ctx, 10*time.Second)
			server.Shutdown(tearDownCtx)
		}()
		server.ConnState = lingerCtx.ConnState
	} else {
		listener, err := net.Listen("tcp", *httpAddr)
		if err != nil {
			log.Fatalf("net.Listen %s: %v", *httpAddr, err)
		}
		ln = listener
	}

	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("ListenAndServe %s: %v", *httpAddr, err)
	}
	cancelFn()
}
```
