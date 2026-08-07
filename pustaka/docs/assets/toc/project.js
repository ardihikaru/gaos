/* ToC part — group "Project".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Project",
  pages: [
    {
      id: "changelog",
      file: "changelog.html",
      title: "Changelog",
      desc: "GAOS's release history, kept in semver.",
      tags: ["changelog", "releases", "semver", "versions"],
      sections: [
        { anchor: "versioning-policy", title: "Versioning policy",
          text: "Semantic versioning: major for breaking changes to the graph schema, the event catalog or a deployment; minor for new capability; patch for fixes. The current version lives in assets/toc.js product.semver." },
        { anchor: "releases", title: "Releases",
          text: "v0.1.0 stands the product up as a deployable thing: Mission Control ported from the prototype to Go and HTMX, the docs engine and landing page vendored in, and dev and prod stacks on product slot 03 behind the WireGuard tunnel." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
