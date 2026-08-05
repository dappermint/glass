package main

import (
	"fmt"

	"github.com/dappermint/glass"
)

func main() {
	g := &Greeter{Name: "gopher"}

	// a direct compiled call site, no reflection anywhere in sight
	fmt.Println("compiled call:      ", g.Greet("hello"))

	in := glass.New()
	must(in.Eval(`patch("main.Greeter", "Greet", func(self, greeting) {
		return "PATCHED " + self.name + " (" + greeting + ")"
	})`))
	fmt.Println("same site, patched: ", g.Greet("hello"))

	must(in.Eval(`unpatch("main.Greeter", "Greet")`))
	fmt.Println("same site, restored:", g.Greet("hello"))
}

func must(v any, err error) {
	if err != nil {
		panic(err)
	}
}
