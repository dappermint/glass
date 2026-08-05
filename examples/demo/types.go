//go:generate go run github.com/dappermint/glass/cmd/gsgen
package main

import "fmt"

// User opts into reflection via gs tags; gsgen finds it and generates
// its Register call in gs_gen.go.
type User struct {
	ID    int    `gs:"id,readonly"`
	Name  string `gs:"name"`
	Email string
	notes string
}

func (u *User) Rename(name string) { u.Name = name }

func (u *User) Greet(greeting string) string {
	return fmt.Sprintf("%s, %s!", greeting, u.Name)
}

// Robot has no tags and no marker; it is registered by hand in main.go.
type Robot struct {
	N int
}

func (r *Robot) Add(deltas ...int) int {
	for _, d := range deltas {
		r.N += d
	}
	return r.N
}
