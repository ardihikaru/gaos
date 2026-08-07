package main

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"
)

// The sequence-diagram renderer, ported from the prototype (lines 2483-2518).
//
// It is the only "chart" in the whole application. There is no charting library
// anywhere in this stack -- pustaka vendors ECharts for its docs, but Mission
// Control draws everything by hand, and this is the one piece of that which is
// data-driven rather than static markup.
//
// Grammar, and it is deliberately tiny:
//
//	Actor -> Actor: message      a call
//	Actor --> Actor: message     a reply (drawn dashed)
//
// Anything that does not match is skipped. A source with no matching line at
// all falls back to a <pre>, which is also what `erd` and `flow` diagrams get:
// showing the text is more honest than drawing a diagram of nothing.

var seqLine = regexp.MustCompile(`^(.+?)\s*(-->|->)\s*(.+?)\s*:\s*(.*)$`)

type seqRow struct {
	From, To, Msg string
	Ret           bool
}

// RenderSequence returns SVG. The return type is template.HTML because the
// caller drops it straight into a template -- and every string that reaches it
// goes through template.HTMLEscapeString first, so the trust is earned rather
// than assumed.
func RenderSequence(src string) template.HTML {
	var rows []seqRow
	var actors []string
	idx := func(name string) int {
		for i, a := range actors {
			if a == name {
				return i
			}
		}
		return -1
	}

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := seqLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		from, to := strings.TrimSpace(m[1]), strings.TrimSpace(m[3])
		if idx(from) < 0 {
			actors = append(actors, from)
		}
		if idx(to) < 0 {
			actors = append(actors, to)
		}
		rows = append(rows, seqRow{From: from, To: to, Msg: strings.TrimSpace(m[4]), Ret: m[2] == "-->"})
	}

	if len(rows) == 0 {
		return template.HTML("<pre>" + template.HTMLEscapeString(src) + "</pre>")
	}

	const colW, top, rowH, pad = 132, 40, 30, 14
	n := len(actors)
	if n < 1 {
		n = 1
	}
	w := pad*2 + n*colW
	h := top + len(rows)*rowH + 24
	x := func(i int) int { return pad + i*colW + colW/2 }

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="seq" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="Sequence diagram">`, w, h, w, h)
	b.WriteString(`<defs><marker id="ah" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">` +
		`<path d="M0 0 L8 4 L0 8 z" fill="var(--run)"/></marker></defs>`)

	for i, a := range actors {
		fmt.Fprintf(&b, `<line class="life" x1="%d" y1="%d" x2="%d" y2="%d"/>`, x(i), top-4, x(i), h-12)
		fmt.Fprintf(&b, `<rect class="box" x="%d" y="8" width="%d" height="24" rx="3"/>`, x(i)-colW/2+8, colW-16)
		fmt.Fprintf(&b, `<text class="actor" x="%d" y="24" text-anchor="middle">%s</text>`,
			x(i), template.HTMLEscapeString(clip(a, 15)))
	}
	for i, r := range rows {
		y := top + 18 + i*rowH
		a, bx := x(idx(r.From)), x(idx(r.To))
		cls := "arrow"
		if r.Ret {
			cls = "arrow ret"
		}
		fmt.Fprintf(&b, `<line class="%s" x1="%d" y1="%d" x2="%d" y2="%d"/>`, cls, a, y, bx, y)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle">%s</text>`,
			(a+bx)/2, y-6, template.HTMLEscapeString(clip(r.Msg, 34)))
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
