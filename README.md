# glass

go, but reflective. register a type once, then construct, inspect, mutate,
call, patch, and reinterpret it at runtime, by string name, from inside go
itself. zero dependencies, zero `unsafe`.

- `gs`: a runtime type registry, the type-by-name lookup go threw away
- `glass`: a go interpreter written in go, running on that registry

together they can swap methods, wrap methods, and patch compiled call sites
while the program runs.

## try it

```
nix run github:dappermint/glass
```

then:

```
u := new("user")
u.name = "gopher"
u.Greet("hello")
```

without nix, `go run ./examples/repl` from a clone does the same.

## the registry

tag a struct and register it:

```go
type User struct {
    ID   int    `gs:"id,readonly"`
    Name string `gs:"name"`
}

func (u *User) Greet(g string) string { return g + ", " + u.Name + "!" }

gs.Register[User]()
```

everything now works by string:

```go
obj, _ := gs.New("main.User")          // returns *User as any
gs.Set(obj, "name", "gopher")          // by tag alias or go name
out, _ := gs.Call(obj, "Greet", "hi")  // dynamic dispatch
```

the day-one api is seven functions: `Register` `New` `Get` `Set` `Call`
`Lookup` `Types`.

to register via codegen instead, put
`//go:generate go run github.com/dappermint/glass/cmd/gsgen` in the package.
every struct with a `gs:` tag (or a `gs:register` doc comment) registers
itself.

### tags

- `gs:"alias"` aliases the field for string access
- `gs:"alias,readonly"` makes `Set` refuse the field
- `gs:"-"` hides the field from the registry
- untagged exported fields work by their go name

### the three laws

from [the laws of reflection](https://go.dev/blog/laws-of-reflection),
enforced as api contracts:

1. interface to reflection object: `Register` walks the type once and caches
   a `Descriptor`, so reflection cost is paid at startup, not per call
2. reflection object to interface: `Call` and `Get` return plain `any`,
   callers never import `reflect`
3. settability: `Set` and pointer-receiver `Call` require a pointer and
   return `ErrNeedPointer` instead of panicking

design lineage: [CallMeMaybe](https://github.com/LaurieWired/CallMeMaybe),
C++26 runtime reflection with the same registry-plus-string-dispatch shape.

## the interpreter

`glass` is a tree-walking interpreter for a go subset. every runtime value
is a `reflect.Value` and every struct access routes through the registry,
so interpreted code can only touch what the host registered or bound:

```go
in := glass.New()
in.Define("double", func(n int) int { return n * 2 })
v, _ := in.Eval(`
u := new("user")
u.name = "gopher"
u.Greet("hello")
`) // "hello, gopher!"
```

supported: literals (composite too: `[]int{1, 2}`, `map[string]int{...}`),
`:=` `=` compound assign, `++` `--`, multi-assign (`a, b = b, a`, `v, err :=
f()`), arithmetic, comparisons including `nil`, short-circuit `&&` `||`,
`if`/`else`, `switch`, `for` with `break`/`continue`, `range` over slices,
maps, strings, and ints, indexing and index assignment, slice expressions,
field get/set, method calls (variadic too), func literals with closures and
multi-value `return`, variadic func literals (`func(self any, rest
...any)`), spread at call sites (`f(xs...)`), host funcs with error-last
handling. builtins: `new` `types` `fields` `methods` `len` `append` `make`
`delete` `print`.

`reflect.Value` already ships `Call`, `Index`, `Len`, `Set`, an entire
operand api. the stdlib has always contained an interpreter runtime with no
parser attached. glass is the parser.

## shards

a shard is an interpreted method attached to a registered type at runtime.
shards dispatch before compiled methods:

```
shard("user", "Shout", func(self, msg) { return self.name + " yells " + msg })
u.Shout("hi")                                               // new method
shard("user", "Shout", func(self, msg) { return msg })      // swap in place
shard("user", "Greet", func(self, g) { return "shadowed" }) // shadow compiled
mend("user", "Greet")                                       // compiled Greet is back
shards(u)                                                   // list them
```

the first param receives the instance, the name is up to you. params are
dynamically typed: `func(self, msg)` and `func(self, msg any)` both parse.
shards live on the `Interp`, so two interpreters can patch the same type
differently.

## advice

CLOS/elisp-style method combination. `before` and `after` observe, `around`
gets a `next` callable and full control. later advice wraps outside earlier:

```
advise("user", "Greet", "around", func(self, next, g) { return "[" + next(g) + "]" })
advise("user", "Greet", "before", func(self, g) { print("greet incoming") })
u.Greet("hi")             // prints, then "[hi, gopher!]"
unadvise("user", "Greet") // removes all advice on Greet, returns the count
```

works on compiled methods and shards alike. an erroring advice aborts the
call.

## pointcuts

instead of naming one method, write a glob and advise everything it
matches. `*` matches any run of characters, everything else is literal:

```
match("*", "Get*")        // ["box.GetName", "crate.GetID", ...]
adviseMatch("*", "*", "before", func(self any, rest ...any) { print("call:", len(rest)) })
unadviseMatch("*", "*")   // bulk removal, returns the count
```

variadic params plus spread make one around advice fit any arity:

```
adviseMatch("box", "*", "around", func(self any, next any, rest ...any) {
    print("in")
    return next(rest...)
})
```

pointcuts quantify over registered methods and this interp's shards at the
moment of the call; methods added later are not advised. `match` shows the
join points before you commit. the same query from go is
`gs.Match(typePat, methodPat)`.

## weaving

everything above only affects calls routed through glass. weaving patches
every caller in the binary, including direct compiled call sites:

1. write the method body as an unexported `xxxImpl`
2. gsgen generates the exported `Xxx` trampoline over it:

```go
func (g *Greeter) greetImpl(greeting string) string { ... }
// generated:
// func (g *Greeter) Greet(greeting string) string {
//     if __p, __ok := gs.Hook(g, "Greet"); __ok { ... }
//     return g.greetImpl(greeting)
// }
```

3. `patch("main.Greeter", "Greet", func(self, g) { ... })` from glass (or
   `gs.Patch` from go) swaps the implementation under every caller.
   `unpatch` restores it

this is the lisp symbol function cell, rebuilt in go. the unpatched fast
path is one atomic load. `examples/weave` demonstrates a compiled call site
changing behaviour at runtime.

## nix

```
nix run github:dappermint/glass    # the repl
nix build .#glass                  # glass + gsgen + glass-weave-demo
nix develop                        # go 1.26, gopls, gotools
direnv allow                       # the dev shell, automatically
```

go 1.26 pinned. tests run in the sandbox as the checkPhase, so a green
`nix build` is also a green test suite.

## limits

- exported fields and methods only, no `unsafe` ever
- dynamic dispatch costs 10-50x a direct call. fine for routing and
  plugins, wrong for hot loops
- interpreter gaps: no goroutines, channels, `defer`, `select`, labels, or
  type assertions yet
- patches fully replace the method (no call-next into the impl), must
  return the method's result count, and run interpreted code on the
  caller's goroutine. keep patched calls single-threaded
- weaving only reaches code gsgen generates, the stdlib stays unpatchable

## glossary

| word  | meaning |
|-------|---------|
| glass | the interpreter, you look through it at your types |
| shard | a piece of behaviour broken off the type, attachable at runtime |
| mend  | put a shadowed method back the way the compiler made it |
| weave | thread the patchable seam through compiled code |

```
 /\_/\
( o.o )   you read the whole thing
 > ^ <    the cat is proud of you
```
