// A REPL over the glass interpreter. Run it interactively, pipe a script to
// stdin, or pass a script file as the first argument.
package main

import (
	"fmt"
	"os"

	"github.com/dappermint/glass/console"
	"github.com/dappermint/glass/glass"
	"github.com/dappermint/glass/gs"
)

type User struct {
	ID    int    `gs:"id,readonly"`
	Name  string `gs:"name"`
	Email string
}

func (u *User) Greet(greeting string) string { return greeting + ", " + u.Name + "!" }

func (u *User) Rename(name string) { u.Name = name }

type Robot struct {
	N int
}

func (r *Robot) Add(deltas ...int) int {
	for _, d := range deltas {
		r.N += d
	}
	return r.N
}

func main() {
	gs.Register[User](gs.WithName("user"))
	gs.Register[Robot](gs.WithName("robot"))

	in := glass.New()

	if len(os.Args) > 1 {
		src, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if v, err := in.Eval(string(src)); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		} else if v != nil {
			fmt.Printf("%#v\n", v)
		}
		return
	}

	fmt.Println("glass repl. builtins: new(name) types() fields(x) methods(x) len append make delete print")
	fmt.Println("shards: shard(type, name, func(self, ...) {...}) mend(type, name) shards(x)")
	fmt.Println("advice: advise(type, name, before|after|around, fn) unadvise(type, name)")
	fmt.Println("cuts:   match(typePat, methodPat) adviseMatch(tPat, mPat, kind, fn) unadviseMatch(tPat, mPat)")
	fmt.Println("weave:  patch(type, name, fn) unpatch(type, name)")
	fmt.Println(`try: u := new("user"); u.name = "gopher"; u.Greet("hello")`)

	if err := console.Run(in, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
