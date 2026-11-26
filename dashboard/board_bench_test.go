package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
)

// The benchmarks of D-094 (docs/designs/urls-and-board.md §5.6), over the
// synthetic product bigBoard builds, at three sizes:
//
//	cd dashboard && go test -run xxx -bench 'Board' -benchmem
//
// They are measurements, not tests: no register row.

var boardSizes = []int{200, 2000, 10000}

// BenchmarkBoard is Store.Board: the unpaged document, every card decorated.
func BenchmarkBoard(b *testing.B) {
	for _, n := range boardSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			_, st := bigBoard(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.Board(ctx, "p-1"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBoardPage is Store.BoardPage with the default window.
func BenchmarkBoardPage(b *testing.B) {
	for _, n := range boardSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			_, st := bigBoard(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.BoardPage(ctx, "p-1", BoardQuery{}, BoardWindow{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBoardCard is Store.BoardCard: the read behind a drag's answer.
func BenchmarkBoardCard(b *testing.B) {
	for _, n := range boardSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			_, st := bigBoard(b, n)
			ctx := context.Background()
			id := fmt.Sprintf("bt-%05d", n/2)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := st.BoardCard(ctx, "p-1", id); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchGet(b *testing.B, srv *server, path string, hdr map[string]string) int {
	h := srv.routes()
	ck := boCookies()
	bytes := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("HX-Request", "true")
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		for _, c := range ck {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			b.Fatalf("GET %s answered %d", path, w.Code)
		}
		bytes = w.Body.Len()
	}
	b.ReportMetric(float64(bytes), "bytes/resp")
	return bytes
}

// BenchmarkBoardTab is opening the Board tab through the router.
func BenchmarkBoardTab(b *testing.B) {
	for _, n := range boardSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			srv, _ := bigBoard(b, n)
			benchGet(b, srv, "/products/p-1?tab=board", nil)
		})
	}
}

// BenchmarkBoardLive is one live refresh after a move: the Board's own route,
// asked with what the browser showed before the move, so the answer is the two
// columns that changed.
func BenchmarkBoardLive(b *testing.B) {
	for _, n := range boardSizes {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			srv, st := bigBoard(b, n)
			before := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/products/p-1?tab=board", nil)
			r.Header.Set("HX-Request", "true")
			for _, c := range boCookies() {
				r.AddCookie(c)
			}
			srv.routes().ServeHTTP(before, r)
			state := stateOf(columnsIn(before.Body.String()), "")
			if _, err := st.MoveCard(context.Background(), "p-1", "bt-00000", CardMove{Column: ColCanceled, Index: -1}); err != nil {
				b.Fatal(err)
			}
			benchGet(b, srv, "/products/p-1/board/live", map[string]string{boardStateHeader: state})
		})
	}
}
