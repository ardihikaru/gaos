package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsBracketError walks JavaScript source and reports the first bracket that
// does not pair up, or "" when every one does. It is a guard, not a parser:
// it skips comments, strings, template literals (text only -- a ${ } inside
// one is read as code) and regular-expression literals, and counts ( [ { on
// everything else. This repository has no JavaScript runtime to parse with.
func jsBracketError(src string) string {
	type open struct {
		ch   byte
		line int
	}
	var stack []open
	line := 1
	// prev is the last significant byte, which decides whether a "/" starts a
	// regular expression or divides.
	var prev byte
	// tmpl holds, for each template literal being read as code inside ${ },
	// the stack depth at which its closing } returns to the literal's text.
	var tmpl []int
	pair := map[byte]byte{')': '(', ']': '[', '}': '{'}

	skipTemplate := func(i int) (int, bool) {
		// i is just past a backtick or a } that closed a ${ }. Returns the
		// index after the closing backtick, or after a ${ with true.
		for i < len(src) {
			switch src[i] {
			case '\\':
				i++
			case '\n':
				line++
			case '`':
				return i + 1, false
			case '$':
				if i+1 < len(src) && src[i+1] == '{' {
					return i + 2, true
				}
			}
			i++
		}
		return i, false
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return fmt.Sprintf("line %d: a comment is never closed", line)
			}
			line += strings.Count(src[i:i+2+end], "\n")
			i += end + 4
		case c == '"' || c == '\'':
			start := line
			i++
			for i < len(src) && src[i] != c {
				if src[i] == '\\' {
					i++
				}
				if i < len(src) && src[i] == '\n' {
					return fmt.Sprintf("line %d: a string is never closed", start)
				}
				i++
			}
			i++
			prev = c
		case c == '`':
			var code bool
			i, code = skipTemplate(i + 1)
			if code {
				stack = append(stack, open{'{', line})
				tmpl = append(tmpl, len(stack))
				prev = '{'
			} else {
				prev = '`'
			}
		case c == '/' && strings.IndexByte("(,=:[!&|?{};+-*%<>~^", prev) >= 0 || c == '/' && prev == 0:
			i++
			class := false
			for i < len(src) && (class || src[i] != '/') {
				switch src[i] {
				case '\\':
					i++
				case '[':
					class = true
				case ']':
					class = false
				case '\n':
					return fmt.Sprintf("line %d: a regular expression is never closed", line)
				}
				i++
			}
			i++
			prev = '/'
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, open{c, line})
			prev = c
			i++
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 {
				return fmt.Sprintf("line %d: %q closes nothing", line, c)
			}
			top := stack[len(stack)-1]
			if top.ch != pair[c] {
				return fmt.Sprintf("line %d: %q closes the %q opened on line %d", line, c, top.ch, top.line)
			}
			if c == '}' && len(tmpl) > 0 && tmpl[len(tmpl)-1] == len(stack) {
				// The } that ends a ${ }: back into the template's text.
				stack = stack[:len(stack)-1]
				tmpl = tmpl[:len(tmpl)-1]
				var code bool
				i, code = skipTemplate(i + 1)
				if code {
					stack = append(stack, open{'{', line})
					tmpl = append(tmpl, len(stack))
					prev = '{'
				} else {
					prev = '`'
				}
				continue
			}
			stack = stack[:len(stack)-1]
			prev = c
			i++
		default:
			prev = c
			i++
		}
	}
	if len(stack) > 0 {
		top := stack[len(stack)-1]
		return fmt.Sprintf("line %d: %q is never closed", top.line, top.ch)
	}
	return ""
}

// D-063's client board stopped loading in v0.44.0 and nobody saw it: a
// function in static/board.js lost its closing brace, the module no longer
// parsed, and app.js's import().catch kept the server-rendered board on screen
// without a word. No Go test read the file. This one does -- every script
// GAOS writes itself must at least pair its brackets.
func TestStaticScriptsHaveBalancedBrackets(t *testing.T) {
	// The guard has to be able to fail: this is the shape of the v0.44.0 bug.
	broken := "function a() {\n  return 1;\nfunction b() {\n  return 2;\n}\n"
	if jsBracketError(broken) == "" {
		t.Fatalf("the bracket guard accepted a function with no closing brace: it would not have caught the board.js bug")
	}
	for _, ok := range []string{
		"var r = /[)}\\]]/.test(s) ? `a ${f({b: 1})} }` : '{';",
		"x = a / b; // (\n/* { */ y = \"[\";",
	} {
		if msg := jsBracketError(ok); msg != "" {
			t.Fatalf("the bracket guard refused valid JavaScript %q: %s", ok, msg)
		}
	}

	files, err := filepath.Glob("static/*.js")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".min.js") {
			continue // vendored, minified, not ours to read
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if msg := jsBracketError(string(b)); msg != "" {
			t.Errorf("%s does not pair its brackets (%s): the browser refuses the whole file, and for board.js that is a silent fall back to the server board", f, msg)
		}
	}
	if checked < 3 {
		t.Fatalf("checked %d scripts under static/, want at least app.js, board.js and live.js", checked)
	}
}
