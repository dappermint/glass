// Package console mounts a glass repl on a unix socket, so a running
// process becomes something you can attach to, inspect, and mend live.
//
// Anyone who can open the socket can drive the interpreter, which reaches
// exactly what the host registered or bound. The socket is created 0600;
// keep it that way.
package console

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/dappermint/glass/glass"
)

// Server owns the socket and the interpreter behind it. Every session shares
// the one interpreter, so state defined in one connection is visible to the
// next; evaluations are serialised.
type Server struct {
	in   *glass.Interp
	ln   net.Listener
	path string

	mu  sync.Mutex // serialises evals and guards out
	out io.Writer

	connMu sync.Mutex
	conns  map[net.Conn]bool
	closed bool
}

// Serve listens on a unix socket at path and runs a repl session per
// connection. A nil interp gets a fresh one. The socket is chmodded to
// 0600, a stale socket left by a dead process is replaced, and print is
// rebound so its output lands on the session that called it.
func Serve(path string, in *glass.Interp) (*Server, error) {
	if in == nil {
		in = glass.New()
	}
	if err := clearStale(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	s := &Server{in: in, ln: ln, path: path, conns: map[net.Conn]bool{}}
	in.Define("print", func(args ...any) {
		w := s.out
		if w == nil {
			w = os.Stdout
		}
		fmt.Fprintln(w, args...)
	})
	go s.accept()
	return s, nil
}

// Interp returns the interpreter behind the socket, for binding live
// objects with Define.
func (s *Server) Interp() *glass.Interp { return s.in }

// Close stops the listener, closes every open session, and removes the
// socket file.
func (s *Server) Close() error {
	s.connMu.Lock()
	if s.closed {
		s.connMu.Unlock()
		return nil
	}
	s.closed = true
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.connMu.Unlock()
	for _, c := range conns {
		c.Close()
	}
	return s.ln.Close()
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.connMu.Lock()
		if s.closed {
			s.connMu.Unlock()
			conn.Close()
			return
		}
		s.conns[conn] = true
		s.connMu.Unlock()
		go s.session(conn)
	}
}

func (s *Server) session(conn net.Conn) {
	defer func() {
		conn.Close()
		s.connMu.Lock()
		delete(s.conns, conn)
		s.connMu.Unlock()
	}()
	fmt.Fprintln(conn, "glass console. sessions share one interpreter; type exit to leave.")
	run(conn, conn, func(src string) (any, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.out = conn
		return s.in.Eval(src)
	})
}

// Run drives a repl over r and w against in. Serve uses the same loop per
// socket connection; the repl example uses it on stdin.
func Run(in *glass.Interp, r io.Reader, w io.Writer) error {
	return run(r, w, in.Eval)
}

func run(r io.Reader, w io.Writer, eval func(string) (any, error)) error {
	sc := bufio.NewScanner(r)
	var buf strings.Builder
	depth := 0
	prompt := func() {
		if depth > 0 {
			fmt.Fprint(w, "... ")
		} else {
			fmt.Fprint(w, ">> ")
		}
	}
	for prompt(); sc.Scan(); prompt() {
		line := sc.Text()
		if depth == 0 && strings.TrimSpace(line) == "exit" {
			return nil
		}
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
		v, err := eval(src)
		if err != nil {
			fmt.Fprintln(w, "error:", err)
			continue
		}
		if v != nil {
			fmt.Fprintf(w, "%#v\n", v)
		}
	}
	return sc.Err()
}

// braceDelta tracks brace/paren nesting outside string and char literals so
// the loop knows when a multi-line block is complete.
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

// clearStale removes a socket file whose listener is gone. A live listener
// or a non-socket file at the path is an error.
func clearStale(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("console: %s exists and is not a socket", path)
	}
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return fmt.Errorf("console: %s already has a live listener", path)
	}
	return os.Remove(path)
}
