package main

import (
	"strconv"
	"strings"
	"testing"
)

func pagerWindow(p Pager) string {
	var b []string
	for _, l := range p.Links {
		if l.Gap {
			b = append(b, "…")
		} else {
			b = append(b, strconv.Itoa(l.N))
		}
	}
	return strings.Join(b, " ")
}

// TC-753: the pager clamps every page it is handed and windows its links.
func TestPaginateClampsAndWindows(t *testing.T) {
	for in, want := range map[string]int{"": 0, "1": 0, "x": 0, "-3": 0, "2": 2, "999999999": maxPage} {
		if got := pageParam(in); got != want {
			t.Errorf("pageParam(%q) = %d, want %d", in, got, want)
		}
	}
	at := func(n int) (string, string) { return "f?page=" + strconv.Itoa(n), "p?page=" + strconv.Itoa(n) }

	if p := paginate(0, 0, 10, at); p.Pages != 1 || p.Page != 1 || p.From != 0 || p.To != 0 || p.Prev != nil || p.Next != nil {
		t.Errorf("an empty list: %+v, want one page, From/To 0/0 and no Prev/Next", p)
	}
	for _, c := range []struct{ page, want, from, to int }{{0, 1, 1, 10}, {99, 3, 21, 25}, {3, 3, 21, 25}, {2, 2, 11, 20}} {
		p := paginate(25, c.page, 10, at)
		if p.Page != c.want || p.From != c.from || p.To != c.to {
			t.Errorf("page %d of 25 items: page %d, %d–%d; want page %d, %d–%d", c.page, p.Page, p.From, p.To, c.want, c.from, c.to)
		}
	}
	if got := sliceFor([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, paginate(12, 2, 10, at)); len(got) != 2 || got[0] != 11 {
		t.Errorf("sliceFor page 2 of 12 = %v, want [11 12]", got)
	}
	for _, c := range []struct {
		pages, on int
		want      string
	}{
		{7, 4, "1 2 3 4 5 6 7"}, {40, 20, "1 … 19 20 21 … 40"}, {40, 3, "1 2 3 4 … 40"}, {1, 1, "1"},
	} {
		if got := pagerWindow(paginate(c.pages*10, c.on, 10, at)); got != c.want {
			t.Errorf("%d pages on page %d: window %q, want %q", c.pages, c.on, got, c.want)
		}
	}
	first, last := paginate(30, 1, 10, at), paginate(30, 3, 10, at)
	if first.Prev != nil || first.Next == nil || first.Next.Get != "f?page=2" || last.Next != nil || last.Prev == nil || last.Prev.Href != "p?page=2" {
		t.Errorf("Prev/Next: first %+v / %+v, last %+v / %+v; want no Prev on page 1, no Next on the last, links through at", first.Prev, first.Next, last.Prev, last.Next)
	}
}
