package console

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dappermint/glass/gs"
)

type dial struct {
	Name string `gs:"name"`
}

func (d *dial) Turn(n int) int { return n * 2 }

func init() { gs.Register[dial](gs.WithName("dial")) }

func sock(t *testing.T) string {
	t.Helper()
	// unix socket paths cap at ~104 bytes on darwin, keep it short
	dir, err := os.MkdirTemp("", "gc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

// expect reads conn until want appears or the deadline hits.
func expect(t *testing.T, conn net.Conn, want string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var got strings.Builder
	buf := make([]byte, 1024)
	for {
		if strings.Contains(got.String(), want) {
			return
		}
		n, err := conn.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("expect %q, got %q (%v)", want, got.String(), err)
		}
	}
}

func send(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

func TestServeEval(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 600", perm)
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expect(t, conn, ">> ")
	send(t, conn, `d := new("dial")`)
	send(t, conn, `d.Turn(21)`)
	expect(t, conn, "42")
}

func TestSharedState(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	one, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, one, ">> ")
	send(t, one, "x := 41")
	send(t, one, "exit")
	expect(t, one, ">> ")
	one.Close()

	two, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	expect(t, two, ">> ")
	send(t, two, "x + 1")
	expect(t, two, "42")
}

func TestPrintGoesToSession(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expect(t, conn, ">> ")
	send(t, conn, `print("through the looking glass")`)
	expect(t, conn, "through the looking glass")
}

func TestMultiline(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expect(t, conn, ">> ")
	send(t, conn, "if true {")
	expect(t, conn, "... ")
	send(t, conn, "print(7)")
	send(t, conn, "}")
	expect(t, conn, "7")
}

func TestEvalError(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	expect(t, conn, ">> ")
	send(t, conn, "nope + 1")
	expect(t, conn, "error:")
	send(t, conn, "1 + 1")
	expect(t, conn, "2")
}

func TestStaleSocketReplaced(t *testing.T) {
	path := sock(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}

	s, err := Serve(path, nil)
	if err != nil {
		t.Fatalf("Serve over stale socket: %v", err)
	}
	defer s.Close()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestLiveListenerRefused(t *testing.T) {
	path := sock(t)
	s, err := Serve(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := Serve(path, nil); err == nil {
		t.Fatal("second Serve on a live socket should fail")
	}
}

func TestNonSocketRefused(t *testing.T) {
	path := sock(t)
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Serve(path, nil); err == nil {
		t.Fatal("Serve over a regular file should fail")
	}
}
