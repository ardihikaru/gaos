package main

import "net/http"

func (s *server) apiSeedProduct(w http.ResponseWriter, r *http.Request) {
	apiErr(w, http.StatusNotImplemented, "not-implemented", "the product seed lands in P3")
}
