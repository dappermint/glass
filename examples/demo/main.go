package main

import (
	"fmt"

	"github.com/dappermint/glass/gs"
)

func init() {
	gs.Register[Robot](gs.WithName("robot"))
}

func main() {
	fmt.Println("registered types:", gs.Types())

	d, _ := gs.Lookup("main.User")
	fmt.Println("User fields: ", d.FieldNames())
	fmt.Println("User methods:", d.MethodNames())

	obj, err := gs.New("main.User")
	check(err)
	check(gs.Set(obj, "name", "gopher"))
	check(gs.Set(obj, "Email", "gopher@example.com"))

	name, err := gs.Get(obj, "name")
	check(err)
	fmt.Println("name via alias:", name)

	out, err := gs.Call(obj, "Greet", "hello")
	check(err)
	fmt.Println("Greet returned:", out[0])

	_, err = gs.Call(obj, "Rename", "gemma")
	check(err)
	fmt.Printf("after Rename: %+v\n", obj)

	fmt.Println("readonly field:", gs.Set(obj, "id", 7))
	fmt.Println("law 3 violation:", gs.Set(User{}, "name", "nope"))

	r, err := gs.New("robot")
	check(err)
	sum, err := gs.Call(r, "Add", 1, 2, 3)
	check(err)
	fmt.Println("robot sum:", sum[0])
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
