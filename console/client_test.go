package console

import (
	"net"
	"strings"
	"testing"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestClientEval(t *testing.T) {
	c := testClient(t)
	out, err := c.Eval("1 + 2")
	if err != nil {
		t.Fatal(err)
	}
	if out != "3" {
		t.Fatalf("out = %q, want 3", out)
	}
}

func TestClientMultiline(t *testing.T) {
	c := testClient(t)
	out, err := c.Eval("if true {\nprint(7)\n}")
	if err != nil {
		t.Fatal(err)
	}
	if out != "7" {
		t.Fatalf("out = %q, want 7", out)
	}
}

func TestClientMultipleChunks(t *testing.T) {
	c := testClient(t)
	out, err := c.Eval("a := 40\na + 2")
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Fatalf("out = %q, want 42", out)
	}
}

func TestClientPrintAndResult(t *testing.T) {
	c := testClient(t)
	out, err := c.Eval(`print("hey"); 1 + 1`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hey\n2" {
		t.Fatalf("out = %q, want hey then 2", out)
	}
}

func TestClientError(t *testing.T) {
	c := testClient(t)
	if _, err := c.Eval("nope + 1"); err == nil {
		t.Fatal("want error for undefined ident")
	}
	out, err := c.Eval("1 + 1")
	if err != nil {
		t.Fatalf("client out of sync after error: %v", err)
	}
	if out != "2" {
		t.Fatalf("out = %q, want 2", out)
	}
}

func TestClientExitRefused(t *testing.T) {
	c := testClient(t)
	if _, err := c.Eval("exit"); err == nil {
		t.Fatal("want error for exit")
	}
	if out, err := c.Eval("3 + 4"); err != nil || out != "7" {
		t.Fatalf("session should survive: out=%q err=%v", out, err)
	}
}

func TestClientSharedState(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	one, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := one.Eval("x := 41"); err != nil {
		t.Fatal(err)
	}
	one.Close()

	two, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	out, err := two.Eval("x + 1")
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Fatalf("out = %q, want 42", out)
	}
}

func TestServeListenerTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := ServeListener(ln, nil)
	defer s.Close()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out, err := c.Eval("2 * 21")
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Fatalf("out = %q, want 42", out)
	}
}

func TestClientRegistryCall(t *testing.T) {
	c := testClient(t)
	out, err := c.Eval(`d := new("dial")` + "\nd.Turn(21)")
	if err != nil {
		t.Fatal(err)
	}
	if out != "42" {
		t.Fatalf("out = %q, want 42", out)
	}
	out, err = c.Eval(`d.name = "gauge"` + "\nd.name")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gauge") {
		t.Fatalf("out = %q, want gauge", out)
	}
}
