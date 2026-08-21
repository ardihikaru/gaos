# Vendored JavaScript

One library. `AUTHORING_SPEC.md` §7 is the contract for using it; this file is
about where it came from and what its licence obliges.

## Apache ECharts 6.1.0

`echarts.min.js` is the `dist/echarts.min.js` from the `echarts` npm package at
**6.1.0**, unmodified. The bundle states its own version — `version:"6.1.0"` —
and is byte-identical to what upstream published, checked on **2026-08-20**:

```
b66b25aeb4df84e33199dc21694014d336d222cbd9deb0e5a7c14bd6aa0d0fd0  echarts.min.js
```

```bash
curl -sSL -o /tmp/echarts.tgz https://registry.npmjs.org/echarts/-/echarts-6.1.0.tgz
tar -xzOf /tmp/echarts.tgz package/dist/echarts.min.js | cmp - echarts.min.js
```

Vendored rather than loaded from a CDN for the same reason as the fonts: no
third-party request on page load, and a docs site that renders identically
offline and in an air-gapped deployment.

### Licence, and what ships with it

| File | Covers |
|---|---|
| `LICENSE-echarts.txt` | Apache License 2.0, plus upstream's own *Subcomponents* appendix |
| `NOTICE-echarts.txt` | the ASF attribution notice — Apache 2.0 §4(d) requires it to be redistributed |
| `LICENSE-d3.txt` | BSD 3-Clause, Mike Bostock. ECharts embeds d3 code in its treemap, tree, graph-force and number utilities, and its `LICENSE` points at this text |

All three were fetched from `apache/echarts` at tag `6.1.0` on 2026-08-20, not
reconstructed.

One more copyright travels inside the bundle itself and needs no file here: the
TypeScript runtime helpers carry a `Copyright (c) Microsoft Corporation` banner
whose **complete** permission grant and warranty disclaimer are inline in
`echarts.min.js`. Minifying it out would strip a licence, which is why the file
is vendored verbatim and never re-minified.

## Adding a library

Put the licence text here in the same change, add a row above, and record the
digest. `deploy/check-stacks.sh` asserts that `echarts.min.js` has both its
`LICENSE-` and its `NOTICE-`; extend that block rather than relying on memory.
