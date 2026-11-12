# Working documents

This tree holds **working** documents: the design gate's policy, the design
documents under review, their review reports, and the sources of published
notes.

It is not the documentation site. The site is `pustaka/docs/`, it is a static
HTML tree served by the vendored Pustaka engine, and every page there is
registered in `pustaka/docs/assets/toc.js`. Nothing in this tree is published,
and nothing here is a docs page.

```
docs/
  rules/      policy: when to do what, and how
  designs/    one design document per feature-sized change, plus its reviews
  articles/   the sources of notes published to mfardiansyah.id
```

The contract for `designs/` is `designs/README.md`. The design gate's policy is
`rules/gaos-design-review.md`, and the always-on rules that point at it live in
`.claude/rules/`.
