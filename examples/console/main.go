// A live service with a console in its side. Run it, curl the endpoint,
// then attach from another terminal and change its state while it serves:
//
//	go run ./examples/console
//	curl localhost:8080
//	nc -U /tmp/glass-console.sock
//	>> stats.greeting = "oi"
//	>> stats.hits
package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/dappermint/glass/console"
	"github.com/dappermint/glass/glass"
	"github.com/dappermint/glass/gs"
)

type Stats struct {
	Hits     int    `gs:"hits"`
	Greeting string `gs:"greeting"`
}

func main() {
	gs.Register[Stats](gs.WithName("stats"))
	stats := &Stats{Greeting: "hullo"}

	in := glass.New()
	in.Define("stats", stats)

	srv, err := console.Serve("/tmp/glass-console.sock", in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer srv.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		stats.Hits++
		fmt.Fprintf(w, "%s, visitor %d\n", stats.Greeting, stats.Hits)
	})
	fmt.Println("serving on :8080, console on /tmp/glass-console.sock")
	fmt.Println("attach: nc -U /tmp/glass-console.sock")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
