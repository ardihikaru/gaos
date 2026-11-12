/* ============================================================
   REGISTRY — the mkdocs.yml equivalent.
   This parent file holds site + product metadata and the list of
   ToC PARTS. Navigation lives in the part files (assets/toc/*.js)
   so each file stays small (< 200 lines) and diffs stay readable.
   The runtime loads parts in order; the Go engine parses the same
   files. Part paths are relative to the docs root.

   This is GAOS's documentation site. The engine underneath it is
   Pustaka, vendored into this repository -- the "Maintaining these
   docs" group explains how to author against it. The version below
   is GAOS's, not the engine's.
   ============================================================ */
window.DOCS = {
  site: {
    name: "GAOS",
    tagline: "A graph-native agent operating system",
    version: "v0.13.0"
  },
  product: {
    name: "GAOS",
    semver: "0.36.0",
    repo: "https://github.com/ardihikaru/gaos"
  },
  parts: [
    "assets/toc/overview.js",
    "assets/toc/product.js",
    "assets/toc/delivery.js",
    "assets/toc/architecture.js",
    "assets/toc/infra.js",
    "assets/toc/guide.js",
    "assets/toc/project.js"
  ],
  nav: []  /* filled by the parts above */
};
