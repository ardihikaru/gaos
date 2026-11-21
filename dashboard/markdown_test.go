package main

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
	"time"
)

// D-082, docs/designs/conversations.md §3.3: model text is rendered escape
// first, structure after, from an allowlist of tags.

const mdExample = "## Plan\n" +
	"1. Read `x`\n" +
	"   - a **b**\n" +
	"     - deep _c_ and snake_case_name\n" +
	"2. Run:\n" +
	"   ```sh\n" +
	"   echo \"<hi>\"\n" +
	"   ```\n" +
	"3. Done: [site](https://example.com/p)\n"

const mdExampleWant = `<div class="md"><h4>Plan</h4><ol><li>Read <code>x</code><ul><li>a <strong>b</strong><ul><li>deep <em>c</em> and snake_case_name</li></ul></li></ul></li><li>Run:<pre class="md-code"><code>echo &#34;&lt;hi&gt;&#34;</code></pre></li><li>Done: <a href="https://example.com/p" rel="noopener noreferrer nofollow" target="_blank">site</a> <span class="md-host">(example.com)</span></li></ol></div>`

func TestMarkdownRendersNestedListsCodeAndInlineCode(t *testing.T) {
	if got := string(renderMarkdown(mdExample)); got != mdExampleWant {
		t.Fatalf("the fixed example rendered\n%s\nwant\n%s", got, mdExampleWant)
	}
	if got := string(renderMarkdown("3. three\n4. four")); !strings.Contains(got, `<ol start="3"><li>three</li><li>four</li></ol>`) {
		t.Fatalf("a list starting at 3 rendered %s; want <ol start=\"3\">", got)
	}
	if got := string(renderMarkdown("- a\n1. b")); !strings.Contains(got, `<ul><li>a</li></ul><ol><li>b</li></ol>`) {
		t.Fatalf("a bullet then a number at the same depth rendered %s; want two lists", got)
	}
}

func TestMarkdownEscapesEverythingTheModelWrote(t *testing.T) {
	for in, mustNot := range map[string]string{
		`<script>alert(1)</script>`:           "<script",
		`<img src=x onerror=alert(1)>`:        "<img",
		`[x](https://a.io/"onmouseover=1)`:    `href="https://a.io/"`,
		`[x](javascript:alert(1))`:            "<a ",
		`[x](http://plain.example)`:           "<a ",
		`[x](data:text/html,hi)`:              "<a ",
		`[x](/relative/path)`:                 "<a ",
		"[x](https://exаmple.com)":            "<a ", // a Cyrillic а in the host
		"[pay‮exe.com](https://ok.io)":        "‮",
		`[a [b] c](https://ok.io)`:            "<a ",
		`![x](https://tracker.example/p.png)`: "<a ",
		"```\n<b>bold</b>\n```":               "<b>",
		"`<i>x</i>`":                          "<i>",
		"> <iframe src=x>":                    "<iframe",
	} {
		got := string(renderMarkdown(in))
		if strings.Contains(got, mustNot) {
			t.Fatalf("%q rendered %s; it must not contain %q -- model text is text", in, got, mustNot)
		}
	}
	if got := string(renderMarkdown("```\nnever closed <x>")); !strings.Contains(got, "&lt;x&gt;</code></pre></div>") {
		t.Fatalf("an unclosed fence rendered %s; want it closed at the end, escaped", got)
	}
	for _, c := range []string{"*", "[", "`", "_", "**", "[a](", "_a"} {
		in := strings.Repeat(c, (500<<10)/len(c))
		start := time.Now()
		renderMarkdown(in)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("500 KiB of %q took %v; the renderer must be linear", c, d)
		}
	}
	big := strings.Repeat("<b>", (600<<10)/3)
	if got := string(renderMarkdown(big)); !strings.HasPrefix(got, `<div class="md"><pre class="md-code"><code>&lt;b&gt;`) {
		t.Fatalf("a 600 KiB body was parsed; want one escaped <pre>")
	}
}

var mdTag = regexp.MustCompile(`<(/?)([a-z0-9]+)((?:\s+[a-z-]+="[^"<>]*")*)>`)

var mdAllowed = map[string]bool{"div": true, "p": true, "br": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "pre": true, "code": true, "strong": true, "em": true,
	"a": true, "span": true, "blockquote": true, "hr": true}

// wellFormed checks the invariant: only allowlisted tags, balanced, and no
// raw '<' outside a tag.
func wellFormed(out string) string {
	var stack []string
	rest := mdTag.ReplaceAllStringFunc(out, func(tag string) string {
		m := mdTag.FindStringSubmatch(tag)
		name := m[2]
		if !mdAllowed[name] {
			stack = append(stack, "!"+name)
			return ""
		}
		if name == "br" || name == "hr" {
			return ""
		}
		if m[1] == "" {
			stack = append(stack, name)
		} else if len(stack) == 0 || stack[len(stack)-1] != name {
			stack = append(stack, "!/"+name)
		} else {
			stack = stack[:len(stack)-1]
		}
		return ""
	})
	for _, s := range stack {
		return "unbalanced or forbidden tag " + s
	}
	if strings.Contains(rest, "<") || strings.Contains(rest, ">") {
		return "a raw < or > outside a tag"
	}
	return ""
}

func TestMarkdownOutputIsWellFormedForAnyInput(t *testing.T) {
	alphabet := []string{"*", "**", "_", "__", "`", "```", "~~~", "[", "]", "(", ")", "](", "https://a.io/x",
		"<", ">", "&", "\"", "'", "#", "## ", "- ", "1. ", "2) ", "> ", "---", "\n", "\n\n", "  ", "    ", "\t",
		"word", "snake_case", "é", "日本", "😀", "‮", "![", "javascript:", "<script>", "</li>"}
	rng := rand.New(rand.NewPCG(20261121, 82))
	for n := 0; n < 2000; n++ {
		var b strings.Builder
		for k := rng.IntN(60); k >= 0; k-- {
			b.WriteString(alphabet[rng.IntN(len(alphabet))])
		}
		in := b.String()
		if why := wellFormed(string(renderMarkdown(in))); why != "" {
			t.Fatalf("input %q rendered %s: %s", in, renderMarkdown(in), why)
		}
	}
}
