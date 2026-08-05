//go:generate go run github.com/dappermint/glass/cmd/gsgen
package main

// Greeter's real logic lives in greetImpl; gsgen weaves the exported Greet
// trampoline over it, which is what makes compiled call sites patchable.
type Greeter struct {
	Name string `gs:"name"`
}

func (g *Greeter) greetImpl(greeting string) string {
	return greeting + ", " + g.Name
}
