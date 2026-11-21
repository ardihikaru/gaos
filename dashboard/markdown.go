package main

import (
	"html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Safe markdown for model-written text (D-082, docs/designs/conversations.md §3.3).
//
// A reply is written by a model, and a model writes whatever the text it read
// steered it to write. So this renderer escapes FIRST and adds structure
// after: every run of text goes through template.HTMLEscapeString before any
// tag is written, every tag is a Go literal from a short allowlist, and this
// file is the only place the conversation sheet builds a template.HTML. No
// HTML the model wrote is ever emitted as HTML; no image is ever fetched; a
// link is a link only when it is https to an ASCII host, and its host is shown
// beside it.
//
// It is linear in its input: block parsing is one pass over the lines, and
// each inline delimiter looks for its closer at most once per line -- after a
// miss it is literal for the rest of that line.

// markdownMax is the size past which a body is shown as one escaped block,
// unparsed: no work proportional to a hostile input beyond one escape.
const markdownMax = 512 << 10

func renderMarkdown(src string) template.HTML {
	if len(src) > markdownMax {
		return template.HTML(`<div class="md"><pre class="md-code"><code>` +
			template.HTMLEscapeString(src) + `</code></pre></div>`)
	}
	r := &mdRenderer{}
	r.b.WriteString(`<div class="md">`)
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		r.line(line)
	}
	r.finish()
	r.b.WriteString(`</div>`)
	return template.HTML(r.b.String())
}

type mdList struct {
	ordered    bool
	markerInd  int // the column the marker sits at
	contentCol int // the column the item's text starts at
}

type mdRenderer struct {
	b     strings.Builder
	lists []mdList

	// the open text block: kind "p", "li" or "quote", and its lines
	textKind  string
	textLines []string

	// an open fence
	fence      bool
	fenceMark  string // "```" or "~~~", at the length it opened with
	fenceStrip int    // the indent removed from each of its lines
	fenceLines []string
}

// indentOf counts leading columns, a tab as 4, and returns the rest.
func indentOf(s string) (int, string) {
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n, s[i:]
		}
	}
	return n, ""
}

// stripCols removes up to n columns of leading indentation.
func stripCols(s string, n int) string {
	i, c := 0, 0
	for i < len(s) && c < n {
		switch s[i] {
		case ' ':
			c++
		case '\t':
			c += 4
		default:
			return s[i:]
		}
		i++
	}
	return s[i:]
}

// listMarker reports a list marker at the start of t: ordered, the number,
// and the marker's length including the space after it.
func listMarker(t string) (ok, ordered bool, n int, length int) {
	if len(t) >= 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' ' {
		return true, false, 0, 2
	}
	i := 0
	for i < len(t) && i < 9 && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' ' {
		num, _ := strconv.Atoi(t[:i])
		return true, true, num, i + 2
	}
	return false, false, 0, 0
}

func fenceMarker(t string) string {
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(t) && t[n] == c {
			n++
		}
		if n >= 3 {
			return t[:n]
		}
	}
	return ""
}

func isRule(t string) bool {
	t = strings.TrimRight(t, " ")
	if len(t) < 3 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != t[0] {
			return false
		}
	}
	return t[0] == '-' || t[0] == '*' || t[0] == '_'
}

func (r *mdRenderer) line(line string) {
	if r.fence {
		_, t := indentOf(line)
		if strings.HasPrefix(t, r.fenceMark) && strings.TrimRight(t[len(fenceMarker(t)):], " ") == "" &&
			fenceMarker(t)[0] == r.fenceMark[0] {
			r.closeFence()
			return
		}
		r.fenceLines = append(r.fenceLines, stripCols(line, r.fenceStrip))
		return
	}
	ind, t := indentOf(line)
	if t == "" {
		r.flush()
		r.closeLists(0)
		return
	}
	if m := fenceMarker(t); m != "" {
		r.flush()
		// A fence at an open item's text column belongs to that item.
		for k := len(r.lists) - 1; k >= 0; k-- {
			if ind >= r.lists[k].contentCol {
				r.closeLists(k + 1)
				r.openFence(m, r.lists[k].contentCol)
				return
			}
		}
		if ind <= 3 {
			r.closeLists(0)
			r.openFence(m, ind)
			return
		}
	}
	if ind <= 3 && t[0] == '#' {
		h := 0
		for h < len(t) && h < 7 && t[h] == '#' {
			h++
		}
		if h <= 6 && h < len(t) && t[h] == ' ' {
			r.flush()
			r.closeLists(0)
			tag := "h4"
			switch {
			case h == 4:
				tag = "h5"
			case h >= 5:
				tag = "h6"
			}
			r.b.WriteString("<" + tag + ">" + inlineMD(strings.TrimSpace(t[h+1:])) + "</" + tag + ">")
			return
		}
	}
	if ind <= 3 && isRule(t) {
		r.flush()
		r.closeLists(0)
		r.b.WriteString("<hr>")
		return
	}
	if ok, ordered, num, mlen := listMarker(t); ok {
		r.flush()
		content := ind + mlen
		// leave every list whose marker sits deeper than this one
		for len(r.lists) > 0 && r.lists[len(r.lists)-1].markerInd > ind {
			r.closeTop()
		}
		switch {
		case len(r.lists) == 0:
			r.openList(ordered, num, ind, content)
		case ind >= r.lists[len(r.lists)-1].markerInd+2:
			r.openList(ordered, num, ind, content) // a child, inside the open item
		case r.lists[len(r.lists)-1].ordered == ordered:
			r.b.WriteString("</li><li>")
			r.lists[len(r.lists)-1].contentCol = content
		default:
			r.closeTop() // the other list type at the same depth: a new list
			r.openList(ordered, num, ind, content)
		}
		r.textKind, r.textLines = "li", []string{t[mlen:]}
		return
	}
	if len(r.lists) > 0 && ind > 0 {
		if r.textKind != "li" {
			r.flush()
			r.textKind = "li"
		}
		r.textLines = append(r.textLines, t)
		return
	}
	if ind <= 3 && t[0] == '>' {
		q := strings.TrimPrefix(strings.TrimPrefix(t, ">"), " ")
		if r.textKind != "quote" {
			r.flush()
			r.closeLists(0)
			r.textKind = "quote"
		}
		r.textLines = append(r.textLines, q)
		return
	}
	if r.textKind != "p" {
		r.flush()
		r.closeLists(0)
		r.textKind = "p"
	}
	r.textLines = append(r.textLines, t)
}

func (r *mdRenderer) openList(ordered bool, num, ind, content int) {
	switch {
	case !ordered:
		r.b.WriteString("<ul><li>")
	case num != 1:
		r.b.WriteString(`<ol start="` + strconv.Itoa(num) + `"><li>`)
	default:
		r.b.WriteString("<ol><li>")
	}
	r.lists = append(r.lists, mdList{ordered: ordered, markerInd: ind, contentCol: content})
}

func (r *mdRenderer) closeTop() {
	r.flush()
	top := r.lists[len(r.lists)-1]
	r.lists = r.lists[:len(r.lists)-1]
	if top.ordered {
		r.b.WriteString("</li></ol>")
	} else {
		r.b.WriteString("</li></ul>")
	}
}

// closeLists closes every open list deeper than keep.
func (r *mdRenderer) closeLists(keep int) {
	for len(r.lists) > keep {
		r.closeTop()
	}
}

func (r *mdRenderer) openFence(mark string, strip int) {
	r.fence, r.fenceMark, r.fenceStrip, r.fenceLines = true, mark, strip, nil
}

func (r *mdRenderer) closeFence() {
	r.b.WriteString(`<pre class="md-code"><code>` +
		template.HTMLEscapeString(strings.Join(r.fenceLines, "\n")) + `</code></pre>`)
	r.fence, r.fenceLines = false, nil
}

func (r *mdRenderer) flush() {
	if len(r.textLines) == 0 {
		r.textKind = ""
		return
	}
	parts := make([]string, len(r.textLines))
	for i, l := range r.textLines {
		parts[i] = inlineMD(strings.TrimRight(l, " "))
	}
	body := strings.Join(parts, "<br>")
	switch r.textKind {
	case "li":
		r.b.WriteString(body)
	case "quote":
		r.b.WriteString("<blockquote>" + body + "</blockquote>")
	default:
		r.b.WriteString("<p>" + body + "</p>")
	}
	r.textKind, r.textLines = "", nil
}

func (r *mdRenderer) finish() {
	if r.fence {
		r.closeFence()
	}
	r.flush()
	r.closeLists(0)
}

// ---------- inline ----------

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordBefore: the rune that ends just before byte i is a letter or digit.
func wordBefore(s string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isWord(r)
}

// wordAt: the rune starting at byte i is a letter or digit.
func wordAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isWord(r)
}

// invisible strips bidi controls and zero-width characters: a link's text and
// host must look like what they are.
func invisible(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069,
			r >= 0x200B && r <= 0x200D, r == 0xFEFF:
			return -1
		}
		return r
	}, s)
}

func asciiOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

type inlineMisses struct {
	code, strongStar, strongUnd, emStar, emUnd, link bool
}

func inlineMD(s string) string {
	var m inlineMisses
	return inlineWith(s, &m)
}

func inlineWith(s string, miss *inlineMisses) string {
	var b strings.Builder
	plain := 0 // start of the pending plain run
	emit := func(upto int) {
		if upto > plain {
			b.WriteString(template.HTMLEscapeString(s[plain:upto]))
		}
	}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '`' && !miss.code:
			j := strings.IndexByte(s[i+1:], '`')
			if j < 0 {
				miss.code = true
				i++
				continue
			}
			emit(i)
			b.WriteString("<code>" + template.HTMLEscapeString(s[i+1:i+1+j]) + "</code>")
			i += j + 2
			plain = i
			continue

		case (c == '*' || c == '_') && i+1 < len(s) && s[i+1] == c:
			missed := &miss.strongStar
			if c == '_' {
				missed = &miss.strongUnd
			}
			if *missed || (c == '_' && wordBefore(s, i)) {
				i += 2
				continue
			}
			end := findCloser(s, i+2, s[i:i+2], c == '_')
			if end < 0 {
				*missed = true
				i += 2
				continue
			}
			if end == i+2 { // "****": nothing to emphasise
				i += 2
				continue
			}
			emit(i)
			inner := inlineMisses{}
			b.WriteString("<strong>" + inlineWith(s[i+2:end], &inner) + "</strong>")
			i = end + 2
			plain = i
			continue

		case c == '*' || c == '_':
			missed := &miss.emStar
			if c == '_' {
				missed = &miss.emUnd
			}
			if *missed || i+1 >= len(s) || s[i+1] == ' ' || (c == '_' && wordBefore(s, i)) {
				i++
				continue
			}
			end := findCloser(s, i+1, s[i:i+1], c == '_')
			if end < 0 {
				*missed = true
				i++
				continue
			}
			emit(i)
			inner := inlineMisses{strongStar: true, strongUnd: true, emStar: true, emUnd: true}
			b.WriteString("<em>" + inlineWith(s[i+1:end], &inner) + "</em>")
			i = end + 1
			plain = i
			continue

		case c == '[' && !miss.link:
			k := strings.Index(s[i+1:], "](")
			if k < 0 {
				miss.link = true
				i++
				continue
			}
			text := s[i+1 : i+1+k]
			urlStart := i + 1 + k + 2
			e := strings.IndexByte(s[urlStart:], ')')
			if e < 0 {
				miss.link = true
				i++
				continue
			}
			raw := s[urlStart : urlStart+e]
			end := urlStart + e + 1
			image := i > 0 && s[i-1] == '!'
			if href, host, ok := safeLink(raw); ok && !image && !strings.Contains(text, "[") {
				emit(i)
				b.WriteString(`<a href="` + template.HTMLEscapeString(href) +
					`" rel="noopener noreferrer nofollow" target="_blank">` +
					template.HTMLEscapeString(invisible(text)) + `</a> <span class="md-host">(` +
					template.HTMLEscapeString(host) + `)</span>`)
				i = end
				plain = i
				continue
			}
			// Not a link we render: the whole thing stays text, as written.
			i = end
			continue
		}
		i++
	}
	emit(len(s))
	return b.String()
}

// findCloser finds delim from start, where the character before it is not a
// space and, for '_', the character after it does not continue a word. One
// forward scan; -1 when there is none on this line.
func findCloser(s string, start int, delim string, wordEnd bool) int {
	for p := start; p < len(s); {
		j := strings.Index(s[p:], delim)
		if j < 0 {
			return -1
		}
		at := p + j
		if at > start && s[at-1] != ' ' && (!wordEnd || !wordAt(s, at+len(delim))) {
			// a single delimiter must not be half of a double one
			if len(delim) == 1 && at+1 < len(s) && s[at+1] == delim[0] {
				p = at + 2
				continue
			}
			return at
		}
		p = at + len(delim)
	}
	return -1
}

// safeLink accepts https to an ASCII host only, and returns the href and the
// host to show beside the link text.
func safeLink(raw string) (string, string, bool) {
	raw = strings.TrimSpace(invisible(raw))
	if raw == "" || strings.ContainsAny(raw, " \t\"'<>") {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", "", false
	}
	host := u.Hostname()
	if host == "" || !asciiOnly(host) {
		return "", "", false
	}
	return u.String(), host, true
}
