package main

// A minimal protobuf wire codec (D-104 §4): enough to speak to TaskMgr's gRPC API without a
// generated library, which the stdlib-only rule forbids (go.mod). It writes varints and
// length-delimited fields, and reads a message into its fields, skipping every wire type it does
// not use, so a field TaskMgr adds later is ignored rather than fatal.

import (
	"encoding/binary"
	"errors"
)

// The wire types.
const (
	pbVarint = 0
	pbI64    = 1
	pbLen    = 2
	pbSGroup = 3
	pbEGroup = 4
	pbI32    = 5
)

var errPBMalformed = errors.New("protobuf: malformed message")

// pbWriter builds one message.
type pbWriter struct{ b []byte }

func (w *pbWriter) tag(field, typ int) { w.b = binary.AppendUvarint(w.b, uint64(field)<<3|uint64(typ)) }

func (w *pbWriter) uvarint(field int, v uint64) {
	w.tag(field, pbVarint)
	w.b = binary.AppendUvarint(w.b, v)
}

func (w *pbWriter) boolean(field int, v bool) {
	n := uint64(0)
	if v {
		n = 1
	}
	w.uvarint(field, n)
}

func (w *pbWriter) bytes(field int, b []byte) {
	w.tag(field, pbLen)
	w.b = binary.AppendUvarint(w.b, uint64(len(b)))
	w.b = append(w.b, b...)
}

func (w *pbWriter) str(field int, s string) { w.bytes(field, []byte(s)) }

// pbField is one field read: a varint's value, or a length-delimited field's bytes.
type pbField struct {
	Num  int
	Type int
	V    uint64
	B    []byte
}

// pbFields reads a whole message. Fixed-width fields and groups are skipped, not returned.
func pbFields(b []byte) ([]pbField, error) {
	var out []pbField
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errPBMalformed
		}
		b = b[n:]
		num, typ := int(key>>3), int(key&7)
		if num <= 0 {
			return nil, errPBMalformed
		}
		var err error
		var f pbField
		f, b, err = pbReadValue(b, num, typ)
		if err != nil {
			return nil, err
		}
		if typ == pbVarint || typ == pbLen {
			out = append(out, f)
		}
	}
	return out, nil
}

// pbReadValue reads the value of a field whose key was just read, and the rest of the input.
func pbReadValue(b []byte, num, typ int) (pbField, []byte, error) {
	f := pbField{Num: num, Type: typ}
	switch typ {
	case pbVarint:
		v, n := binary.Uvarint(b)
		if n <= 0 {
			return f, nil, errPBMalformed
		}
		f.V = v
		return f, b[n:], nil
	case pbI64:
		if len(b) < 8 {
			return f, nil, errPBMalformed
		}
		return f, b[8:], nil
	case pbI32:
		if len(b) < 4 {
			return f, nil, errPBMalformed
		}
		return f, b[4:], nil
	case pbLen:
		l, n := binary.Uvarint(b)
		if n <= 0 || l > uint64(len(b)-n) {
			return f, nil, errPBMalformed
		}
		f.B = b[n : n+int(l)]
		return f, b[n+int(l):], nil
	case pbSGroup:
		// A group runs to its matching end-group key; nested groups are skipped whole.
		for {
			key, n := binary.Uvarint(b)
			if n <= 0 {
				return f, nil, errPBMalformed
			}
			b = b[n:]
			inNum, inTyp := int(key>>3), int(key&7)
			if inTyp == pbEGroup {
				if inNum != num {
					return f, nil, errPBMalformed
				}
				return f, b, nil
			}
			var err error
			if _, b, err = pbReadValue(b, inNum, inTyp); err != nil {
				return f, nil, err
			}
		}
	}
	return f, nil, errPBMalformed
}

// pbMsg is a read message, indexed by field number.
type pbMsg map[int][]pbField

func pbParse(b []byte) (pbMsg, error) {
	fs, err := pbFields(b)
	if err != nil {
		return nil, err
	}
	m := pbMsg{}
	for _, f := range fs {
		m[f.Num] = append(m[f.Num], f)
	}
	return m, nil
}

// last is the last occurrence of a field, as protobuf's merge rule has it.
func (m pbMsg) last(n int) (pbField, bool) {
	fs := m[n]
	if len(fs) == 0 {
		return pbField{}, false
	}
	return fs[len(fs)-1], true
}

func (m pbMsg) str(n int) string {
	if f, ok := m.last(n); ok && f.Type == pbLen {
		return string(f.B)
	}
	return ""
}

func (m pbMsg) uint(n int) uint64 {
	if f, ok := m.last(n); ok && f.Type == pbVarint {
		return f.V
	}
	return 0
}

func (m pbMsg) boolean(n int) bool { return m.uint(n) != 0 }

// msg is a nested message field, or an empty one.
func (m pbMsg) msg(n int) (pbMsg, error) {
	if f, ok := m.last(n); ok && f.Type == pbLen {
		return pbParse(f.B)
	}
	return pbMsg{}, nil
}

// msgs is every occurrence of a repeated message field.
func (m pbMsg) msgs(n int) ([]pbMsg, error) {
	var out []pbMsg
	for _, f := range m[n] {
		if f.Type != pbLen {
			continue
		}
		sub, err := pbParse(f.B)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, nil
}
