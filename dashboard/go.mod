// Stdlib only. There is no `require` block and there is no go.sum, and that is
// the point: pustaka next door builds the same way, and a service with no
// dependency graph has no dependency graph to audit, patch or wait on.
//
// If a third-party package ever looks necessary here, read pustaka/main.go
// first -- html/template, net/http, embed and math/rand/v2 cover everything
// this dashboard does.
module gaos/dashboard

go 1.24
