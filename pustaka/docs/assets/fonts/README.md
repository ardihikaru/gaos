# Vendored fonts

Three families, latin subset, woff2 only. Vendored deliberately — `fonts.css`
explains why: no third-party request on page load, identical rendering offline
and in air-gapped deployments, and no layout shift from a blocked font CDN.

Every file here came from a [Fontsource](https://fontsource.org) package. The
filenames are Fontsource's own convention and are not ours to rename:

    <family>-<subset>-<weight>-<style>.woff2

## What is here, and where it came from

| Family | Licence | Upstream package | Version |
|---|---|---|---|
| Atkinson Hyperlegible | SIL OFL 1.1 | `@fontsource/atkinson-hyperlegible` | 5.3.0 |
| Bricolage Grotesque | SIL OFL 1.1 | `@fontsource/bricolage-grotesque` | 5.3.0 |
| JetBrains Mono | SIL OFL 1.1 | `@fontsource/jetbrains-mono` | 5.3.0 |

The full licence text for each sits beside the files it covers, as
`LICENSE-<family>.txt`, each retaining its own copyright line — Braille
Institute of America for Atkinson Hyperlegible, the Bricolage Grotesque Project
Authors, the JetBrains Mono Project Authors. Keeping those texts is an
obligation of redistribution, not a courtesy: they ship in the repository, and
`deploy/Dockerfile.pustaka` copies this whole directory into the published
image, so they travel with the fonts wherever the fonts go.

The licence texts were fetched from the upstream repository
(`fontsource/font-files`, `fonts/google/<family>/LICENSE`) on **2026-08-20**,
not reconstructed.

## Verifying the files are what upstream published

Every `.woff2` here is byte-identical to the file of the same name inside its
`@fontsource` package at 5.3.0, checked on 2026-08-20:

```bash
cd pustaka/docs/assets/fonts
for f in *.woff2; do
  pkg="${f%%-latin-*}"
  curl -sSL -o /tmp/up.woff2 "https://cdn.jsdelivr.net/npm/@fontsource/$pkg@5.3.0/files/$f"
  cmp /tmp/up.woff2 "$f" && echo "  match $f"
done
```

Or against the recorded digests:

```
bc8825fd435d4aa8e31449937826d583a7daaae15a83832c91b38375131ebf08  atkinson-hyperlegible-latin-400-italic.woff2
d64ba838ef5472bba248620ec4fd8b5aa7cf0db2908e0bb230600caf279ba7bc  atkinson-hyperlegible-latin-400-normal.woff2
140e2bd25a7315c8a062508391426b0d8c3297400c947b8d847be28f73a199f0  atkinson-hyperlegible-latin-700-normal.woff2
b62688707e0820a9cf2a98e9b0349fbb348fd17f76b70a05b53e7a668e3f406f  bricolage-grotesque-latin-500-normal.woff2
b34fc8c1ef0ac8798455ac2979eae4b4f90f0d327e3584d1032fa77a8a9a66ca  bricolage-grotesque-latin-600-normal.woff2
4c373ce3c1cca41c864eb3e27c059a59fc6310547ab9c9b6cd780d387ba24206  bricolage-grotesque-latin-700-normal.woff2
3e1b5f0a56ee995b7c1445bc54e6ec98c5dffb585ef5c4baf86731cf68e27c61  bricolage-grotesque-latin-800-normal.woff2
cb182feeed4d798ff6961d3c79f7026279448fca0676438aaecb21f3fc39553a  jetbrains-mono-latin-500-normal.woff2
400c6bfda18d5d14acad1c15d6dcb9f8e13c015e7286317e0b9a482539bef147  jetbrains-mono-latin-600-normal.woff2
d0d4e818808f2a0ba39b2b09d1989366f63494e295f003c7ef436697378507e8  jetbrains-mono-latin-700-normal.woff2
```

## Adding or replacing a face

Add the `.woff2`, add its `@font-face` block to `../fonts.css`, and — if the
family is new — put its `LICENSE-<family>.txt` here in the same change and add a
row above. `deploy/check-stacks.sh` asserts that every family present has its
licence text, so a font added without one fails the pre-flight before it can
reach a deploy.
