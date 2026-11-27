package main

// resp.go is a minimal RESP2 client for the traffic store (D-107, design
// docs/designs/traffic-monitor.md §6). It is written here so the module stays
// stdlib-only: the binary that would carry a Redis library holds every GAOS
// credential.
//
// It sends four commands and nothing else: AUTH once per connection, then XADD,
// XTRIM and XREVRANGE, which may be pipelined. Every reply is parsed under fixed
// bounds, and nothing is allocated from a declared length before that length is
// checked. Any I/O, parse or AUTH error closes the connection; the caller
// redials. A respConn is not safe for concurrent use: the writer owns one, the
// page's reader another under its own mutex.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// The parser's bounds (§6).
const (
	respMaxLine   = 1 << 10  // a simple string, an error, an integer or a length line
	respMaxBulk   = 64 << 10 // a bulk string
	respMaxArray  = 50000    // the elements of one array
	respMaxDepth  = 3        // arrays inside arrays
	respMaxErrLen = 80       // an error reply, as a Go error
)

// errRESPAuth is the one error an AUTH refusal becomes. It never carries the
// password or the server's words.
var errRESPAuth = errors.New("traffic: the store refused the credential")

// respValue is one parsed reply. kind is the RESP2 type byte: '+', '-', ':',
// '$' or '*'. A null bulk string or array has null set.
type respValue struct {
	kind byte
	str  string
	num  int64
	arr  []respValue
	null bool
}

// Err is the reply as a Go error when it is an error reply, else nil. The text
// is cut to 80 bytes.
func (v respValue) Err() error {
	if v.kind != '-' {
		return nil
	}
	return respError(v.str)
}

type respError string

func (e respError) Error() string { return string(e) }

// respConn is one connection to the store.
type respConn struct {
	c  net.Conn
	r  *bufio.Reader
	w  *bufio.Writer
	op time.Duration // the deadline of one exchange
}

// dialRESP connects and signs in as user with pass. The password never appears
// in an error.
func dialRESP(addr, user, pass string, dial, op time.Duration) (*respConn, error) {
	c, err := net.DialTimeout("tcp", addr, dial)
	if err != nil {
		return nil, err
	}
	rc := &respConn{c: c, r: bufio.NewReaderSize(c, 4096), w: bufio.NewWriterSize(c, 16<<10), op: op}
	if err := rc.auth(user, pass); err != nil {
		rc.Close()
		return nil, err
	}
	return rc, nil
}

func (rc *respConn) auth(user, pass string) error {
	rs, err := rc.pipeline([][]string{{"AUTH", user, pass}})
	if err != nil {
		return err
	}
	if rs[0].kind != '+' || rs[0].str != "OK" {
		return errRESPAuth
	}
	return nil
}

// Close closes the connection. A nil connection is closed already.
func (rc *respConn) Close() {
	if rc != nil && rc.c != nil {
		_ = rc.c.Close()
	}
}

// appendCommand encodes one command as a RESP2 array of bulk strings.
func appendCommand(b []byte, args []string) []byte {
	b = append(b, '*')
	b = strconv.AppendInt(b, int64(len(args)), 10)
	b = append(b, '\r', '\n')
	for _, a := range args {
		b = append(b, '$')
		b = strconv.AppendInt(b, int64(len(a)), 10)
		b = append(b, '\r', '\n')
		b = append(b, a...)
		b = append(b, '\r', '\n')
	}
	return b
}

// pipeline sends every command, then reads one reply per command, in order. An
// error reply is a value (its Err is set), not a failure of the pipeline: one
// refused XADD must not cost the others. Any I/O or parse error closes the
// connection and is returned; the connection is then unusable.
func (rc *respConn) pipeline(cmds [][]string) ([]respValue, error) {
	if rc == nil || rc.c == nil {
		return nil, errors.New("traffic: no connection")
	}
	if rc.op > 0 {
		_ = rc.c.SetDeadline(time.Now().Add(rc.op))
	}
	var buf []byte
	for _, c := range cmds {
		buf = appendCommand(buf[:0], c)
		if _, err := rc.w.Write(buf); err != nil {
			rc.Close()
			return nil, err
		}
	}
	if err := rc.w.Flush(); err != nil {
		rc.Close()
		return nil, err
	}
	out := make([]respValue, 0, len(cmds))
	for range cmds {
		v, err := readReply(rc.r, 0)
		if err != nil {
			rc.Close()
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// readLine reads one CRLF-terminated line of at most respMaxLine bytes, without
// the CRLF.
func readLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		frag, err := r.ReadSlice('\n')
		if len(line)+len(frag) > respMaxLine+2 {
			return "", errors.New("traffic: a reply line is too long")
		}
		line = append(line, frag...)
		if err == nil {
			break
		}
		if err != bufio.ErrBufferFull {
			return "", err
		}
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", errors.New("traffic: a reply line does not end in CRLF")
	}
	return string(line[:len(line)-2]), nil
}

// readLen parses a length line's number: -1 (null) or 0 … max.
func readLen(s string, max int) (int, error) {
	n, err := strconv.Atoi(s)
	switch {
	case err != nil:
		return 0, errors.New("traffic: a length is not a number")
	case n < -1:
		return 0, errors.New("traffic: a length is negative")
	case n > max:
		return 0, fmt.Errorf("traffic: a length of %d is over the bound of %d", n, max)
	}
	return n, nil
}

// readReply parses one reply at the given nesting depth.
func readReply(r *bufio.Reader, depth int) (respValue, error) {
	line, err := readLine(r)
	if err != nil {
		return respValue{}, err
	}
	if line == "" {
		return respValue{}, errors.New("traffic: an empty reply line")
	}
	kind, rest := line[0], line[1:]
	switch kind {
	case '+':
		return respValue{kind: '+', str: rest}, nil
	case '-':
		if len(rest) > respMaxErrLen {
			rest = rest[:respMaxErrLen]
		}
		return respValue{kind: '-', str: rest}, nil
	case ':':
		n, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return respValue{}, errors.New("traffic: an integer reply is not a number")
		}
		return respValue{kind: ':', num: n}, nil
	case '$':
		n, err := readLen(rest, respMaxBulk)
		if err != nil {
			return respValue{}, err
		}
		if n == -1 {
			return respValue{kind: '$', null: true}, nil
		}
		b := make([]byte, n+2)
		if _, err := io.ReadFull(r, b); err != nil {
			return respValue{}, err
		}
		if b[n] != '\r' || b[n+1] != '\n' {
			return respValue{}, errors.New("traffic: a bulk string does not end in CRLF")
		}
		return respValue{kind: '$', str: string(b[:n])}, nil
	case '*':
		if depth >= respMaxDepth {
			return respValue{}, errors.New("traffic: a reply is nested too deep")
		}
		n, err := readLen(rest, respMaxArray)
		if err != nil {
			return respValue{}, err
		}
		if n == -1 {
			return respValue{kind: '*', null: true}, nil
		}
		// n is within the bound; still, the slice grows with what arrives, not
		// with what was declared.
		arr := make([]respValue, 0, min(n, 64))
		for i := 0; i < n; i++ {
			v, err := readReply(r, depth+1)
			if err != nil {
				return respValue{}, err
			}
			arr = append(arr, v)
		}
		return respValue{kind: '*', arr: arr}, nil
	}
	return respValue{}, fmt.Errorf("traffic: an unknown reply type %q", kind)
}
