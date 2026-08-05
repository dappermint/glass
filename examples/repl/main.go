// A REPL over the glass interpreter. Run it interactively, pipe a script to
// stdin, or pass a script file as the first argument.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/dappermint/glass"
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

	fmt.Println("glass repl. builtins: new(name) types() fields(x) methods(x) len(x) print(...)")
	fmt.Println("shards: shard(type, name, func(self, ...) {...}) mend(type, name) shards(x)")
	fmt.Println("advice: advise(type, name, before|after|around, fn) unadvise(type, name)")
	fmt.Println("weave:  patch(type, name, fn) unpatch(type, name)")
	fmt.Println(`try: u := new("user"); u.name = "gopher"; u.Greet("hello")`)

	sc := bufio.NewScanner(os.Stdin)
	var buf strings.Builder
	depth := 0
	prompt := func() {
		if depth > 0 {
			fmt.Print("... ")
		} else {
			fmt.Print(">> ")
		}
	}
	for prompt(); sc.Scan(); prompt() {
		line := sc.Text()
		buf.WriteString(line)
		buf.WriteString("\n")
		if depth += braceDelta(line); depth > 0 {
			continue
		}
		src := buf.String()
		buf.Reset()
		depth = 0
		if strings.TrimSpace(src) == "" {
			continue
		}
		v, err := in.Eval(src)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		if v != nil {
			fmt.Printf("%#v\n", v)
		}
	}
}

// braceDelta tracks brace/paren nesting outside string and char literals so
// the repl knows when a multi-line block is complete.
func braceDelta(line string) int {
	depth := 0
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			escaped = false
		case quote != 0:
			if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'' || r == '`':
			quote = r
		case r == '{' || r == '(':
			depth++
		case r == '}' || r == ')':
			depth--
		}
	}
	return depth
}
