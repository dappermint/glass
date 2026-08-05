# glass

go, but reflective. register a type once, then construct, inspect, mutate,
call, patch, and *reinterpret* it at runtime, by string name, from inside go
itself. zero dependencies, zero `unsafe`.

## tl;dr

1. `gs` is a runtime type registry: the type-by-name lookup go threw away
2. `glass` is a go interpreter written in go, running on that registry
3. together they do the whole lisp trick: swap methods, wrap methods, and
   patch *compiled call sites* while the program runs

## try it in 30 seconds

```
nix run github:dappermint/glass
```

then type:

```
u := new("user")
u.name = "gopher"
u.Greet("hello")
```

no nix? `go run ./examples/repl` from a clone does the same thing.

## skip to

- [quick start](#quick-start-the-library) — the registry, 3 steps
- [the three laws](#the-three-laws-skippable-theory) — theory, safe to skip
- [the interpreter](#the-interpreter-the-cool-part) — go evals go
- [shards](#shards-the-very-cool-part) — swappable runtime methods
- [advice](#advice-defadvice-for-go) — before/after/around wrapping
- [weaving](#weaving-the-wait-what-part) — patch compiled call sites
- [nix](#nix) / [honest limits](#honest-limits) / [glossary](#glossary-the-metaphor-fully-committed)

## quick start (the library)

1. tag a struct and register it:

```go
type User struct {
    ID   int    `gs:"id,readonly"`
    Name string `gs:"name"`
}

func (u *User) Greet(g string) string { return g + ", " + u.Name + "!" }

gs.Register[User]()
```

2. now everything works by string:

```go
obj, _ := gs.New("main.User")          // returns *User as any
gs.Set(obj, "name", "gopher")          // by tag alias or go name
out, _ := gs.Call(obj, "Greet", "hi")  // dynamic dispatch
```

3. done. that's the whole api surface you need on day one:
   `Register` `New` `Get` `Set` `Call` `Lookup` `Types`

prefer codegen over manual registration? put
`//go:generate go run github.com/dappermint/glass/cmd/gsgen` in the package
and every struct with a `gs:` tag (or a `gs:register` doc comment) registers
itself.

## the three laws (skippable theory)

from [the laws of reflection](https://go.dev/blog/laws-of-reflection),
enforced as api contracts instead of documentation:

1. interface → reflection object: `Register` walks the type once, caches a
   `Descriptor`. reflection cost is paid at startup, not per call
2. reflection object → interface: `Call` and `Get` return plain `any`,
   callers never import `reflect`
3. settability: `Set` and pointer-receiver `Call` require a pointer and
   return `ErrNeedPointer` instead of panicking

design lineage: [CallMeMaybe](https://github.com/LaurieWired/CallMeMaybe)
(C++26 runtime reflection) for the registry-plus-string-dispatch shape.

## tags

- `gs:"alias"` — string alias for field access
- `gs:"alias,readonly"` — `Set` refuses this field
- `gs:"-"` — invisible to the registry
- untagged exported fields still work by their go name

## the interpreter (the cool part)

`glass` is a tree-walking interpreter for a go subset. every runtime value
is a `reflect.Value`, every struct access routes through the registry, so
interpreted code can only touch what the host registered or bound:

```go
in := glass.New()
in.Define("double", func(n int) int { return n * 2 })
v, _ := in.Eval(`
u := new("user")
u.name = "gopher"
u.Greet("hello")
`) // "hello, gopher!"
```

supported: literals, `:=` `=` compound assign, `++` `--`, arithmetic,
comparisons, short-circuit `&&` `||`, `if`/`else`, `for` with
`break`/`continue`, indexing, field get/set, method calls (variadic too),
func literals with real closures and `return`, host funcs with error-last
handling. builtins: `new` `types` `fields` `methods` `len` `print`.

the fun realization: `reflect.Value` already ships `Call`, `Index`, `Len`,
`Set`, an entire operand api. the stdlib has always contained an interpreter
runtime with no parser attached. glass is the parser.

## shards (the very cool part)

a shard is an interpreted method attached to a registered type at runtime.
shards dispatch *before* compiled methods:

```
shard("user", "Shout", func(self, msg) { return self.name + " yells " + msg })
u.Shout("hi")                             // new method, out of nowhere
shard("user", "Shout", func(self, msg) { return msg })   // swap in place
shard("user", "Greet", func(self, g) { return "shadowed" })  // shadow compiled
mend("user", "Greet")                     // compiled Greet is back
shards(u)                                 // list them
```

first param receives the instance, name it whatever. params are dynamically
typed: `func(self, msg)` and `func(self, msg any)` both parse. shards live
on the `Interp`, so two interpreters can patch the same type differently.

## advice (defadvice for go)

CLOS/elisp-style method combination. `before` and `after` observe, `around`
gets a `next` callable and full control. later advice wraps outside earlier:

```
advise("user", "Greet", "around", func(self, next, g) { return "[" + next(g) + "]" })
advise("user", "Greet", "before", func(self, g) { print("greet incoming") })
u.Greet("hi")             // prints, then "[hi, gopher!]"
unadvise("user", "Greet") // peel it all off, returns the count
```

works on compiled methods and shards alike. an erroring advice aborts the
call.

## weaving (the "wait, what" part)

everything above only affects calls routed through glass. weaving patches
**every caller in the binary, including direct compiled call sites**:

1. write your method body as an unexported `xxxImpl`
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
   `gs.Patch` from go) now swaps the implementation under every caller.
   `unpatch` restores it

this is the lisp symbol function cell, rebuilt in go. the unpatched fast
path costs one atomic load. `examples/weave` is a compiled call site
changing behavior at runtime, run it.

## nix

```
nix run github:dappermint/glass    # the repl
nix build .#glass                  # glass + gsgen + glass-weave-demo
nix develop                        # go 1.26, gopls, gotools
direnv allow                       # same shell, automatically, forever
```

builds with go 1.26 pinned. tests run inside the sandbox as the checkPhase,
so a green `nix build` is also a green test suite.

## honest limits

- exported fields and methods only, no `unsafe` ever
- dynamic dispatch costs ~10-50x a direct call. routing and plugins yes,
  hot loops no
- interpreter gaps: no goroutines, channels, composite literals, or
  multi-assign yet
- patches fully replace (no call-next into the impl), must return the
  method's result count, and run interpreted code on the caller's
  goroutine, keep patched calls single-threaded
- weaving only reaches code gsgen generates, stdlib stays unpatchable

## glossary (the metaphor, fully committed)

| word | meaning |
|---|---|
| glass | the interpreter. you look *through* it at your types |
| shard | a broken-off piece of behavior, attachable at runtime |
| mend | put the glass back the way the compiler made it |
| weave | thread the patchable seam through compiled code |

```
 /\_/\
( o.o )   you read the whole thing
 > ^ <    the cat is proud of you
```
