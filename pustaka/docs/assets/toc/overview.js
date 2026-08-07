/* ToC part — group "Overview".
   Loaded by assets/toc.js (parts list). Keep this file under 200 lines;
   when it grows past that, split the group or move pages to a new part.
   Entries MUST keep the key order: id, file, title, desc, tags, sections. */
window.DOCS.nav.push({
  group: "Overview",
  pages: [
    {
      id: "home",
      file: "index.html",
      title: "Home",
      desc: "What GAOS is, which parts are designed versus built, and the numbers behind both.",
      tags: ["home", "overview", "landing", "graph", "agents", "search"],
      sections: [
        { anchor: "search-demo", title: "Answers before you finish typing",
          text: "Embedded live search running the real engine with AND semantics and prefix matching over titles, tags and section text." },
        { anchor: "what-it-is", title: "Three things, and the first one is a claim",
          text: "An orchestration model, an operator surface, and the infrastructure both run on. Only the first is finished as a design; every page says which parts are not built yet." },
        { anchor: "numbers", title: "Measured, not claimed",
          text: "33 dashboard tests with no dependencies and no go.sum, 60 graph types across vertices edges and documents, 27 event names in a closed catalog, and one published port per deployed tier." },
        { anchor: "why", title: "Why a graph",
          text: "A prompt chain has no addressable intermediate state, the orchestrator holds nothing so a restart loses nothing, an unwritten event is an unhappened change, and a vague record is refused rather than guessed at." },
        { anchor: "explore", title: "Explore the docs",
          text: "Filterable index of every page: the system design, the ArcadeDB model, the three infrastructure pages, and the authoring guides for this site." }
      ]
    }
    /* pustaka:insert — `pustaka new` adds entries above this marker */
  ]
});
