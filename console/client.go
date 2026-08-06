package console

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Client drives a console session programmatically, for tests and tooling.
// In-process tests already hold the Interp and can Eval it directly; a
// Client tests the console through its real transport.
//
// The client parses the session's prompts, which works because the server
// emits exactly one prompt per input line. Output lines that themselves
// begin with a prompt token would confuse it; the console never emits any.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to a console socket and consumes the banner.
func Dial(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return NewClient(conn)
}

// NewClient wraps an existing connection to a console server and consumes
// the banner. Use it when the server sits on a listener Dial cannot reach,
// like tcp or a net.Pipe.
func NewClient(conn net.Conn) (*Client, error) {
	c := &Client{conn: conn, r: bufio.NewReader(conn)}
	if _, err := c.readTo(1); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Eval sends src and returns everything the session printed back, prompts
// stripped and the trailing newline trimmed. Results arrive in their %#v
// form, print output as printed. A chunk the interpreter rejected surfaces
// as an error carrying the first error line; later chunks still ran.
func (c *Client) Eval(src string) (string, error) {
	src = strings.TrimRight(src, "\n")
	lines := strings.Split(src, "\n")
	depth := 0
	for _, l := range lines {
		if depth == 0 && strings.TrimSpace(l) == "exit" {
			return "", errors.New("console: exit would end the session, use Close")
		}
		depth += braceDelta(l)
	}
	if _, err := c.conn.Write([]byte(src + "\n")); err != nil {
		return "", err
	}
	out, err := c.readTo(len(lines))
	if err != nil {
		return out, err
	}
	for _, line := range strings.Split(out, "\n") {
		if msg, ok := strings.CutPrefix(line, "error: "); ok {
			return out, errors.New(msg)
		}
	}
	return out, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// readTo consumes the stream until prompts prompts have gone by and returns
// the output between them. Every server write is either a prompt or a
// newline-terminated line, so no framing beyond that is needed.
func (c *Client) readTo(prompts int) (string, error) {
	var out strings.Builder
	for prompts > 0 {
		b, err := c.r.Peek(3)
		if err != nil {
			return out.String(), fmt.Errorf("console: session ended mid-read: %w", err)
		}
		if string(b) == ">> " {
			c.r.Discard(3)
			prompts--
			continue
		}
		if b4, err := c.r.Peek(4); err == nil && string(b4) == "... " {
			c.r.Discard(4)
			prompts--
			continue
		}
		line, err := c.r.ReadString('\n')
		out.WriteString(line)
		if err != nil {
			return out.String(), fmt.Errorf("console: session ended mid-read: %w", err)
		}
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
