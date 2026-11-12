This repository is a platform that runs agents, and it has been running them without ever asking whether the plan was any good. Every task left GAOS as a brief — the product record, the acceptance criteria, the standing rules — and came back as a handoff block. Between those two points there was no moment at which somebody with no stake in the conversation read the plan and said "this is how it should be built".

That is not a complaint about the agents. It is a complaint about the shape of the loop. GAOS has a Blueprint, a readiness rubric, an egress pass and a handoff contract, and all four of them act on work that already exists. The readiness rubric judges a product record. The egress pass screens a packet. The handoff contract judges a reply. Nothing ever judged a *design*.

So I did what the reference implementation asks for. I brought in Google's Elephant-Goldfish Model, wrote a design document for the change that introduced it, and had five fresh reviewers read that document alone and attack it.

The result was uncomfortable, and it was not uncomfortable where I expected.

## The method, in one paragraph

The Elephant-Goldfish Model is not a model and not an agent. It is Markdown. The agent holding the project context drafts a specification; separate reviewers with no memory of that conversation get the specification and nothing else, and try to break it. The context that produced the plan is the context that cannot see the plan's blind spots.

The reviewer that found the most in my own workspace was not the cleverest one. It was the one that refused to stop asking whether the change was bigger than the defect.

## What the reviewers found in a workspace I thought I knew

I went in expecting to find one or two things. I found ten in the workspace, and then the reviewers turned the audit I had written about them and found four more — in the audit itself.

The first finding was the one that made me rewrite the document. My change list had a summary table and per-section headings, and they did not agree with each other. The table counted files; the headings counted numbered entries. One directory was counted as a file in the table while the paragraph under it said directories were not counted. A file I had filed under "Modified" did not exist. The document said, in bold, that the counts were derived rather than asserted — and its own counts were asserted. Three independent reviewers found this, each from a different direction, which is the strongest signal a review can produce.

The second finding was that my headline evidence used two measurement bases at once. I had written that the agent workspace was "164 KB" and then, three lines later, that one file was "27,509 bytes". The 164 KB was a block count from `du`, which rounds every file up to 4 KB; the honest number was 95,762 bytes. I had also written that a vendored skill "carries 20 mentions of React-family technology" — and no single vocabulary of the word produces both the 20 and the 2 I compared it against. The conclusion survived; the numbers did not. A claim you cannot re-derive is not evidence, and the review made that plain in a way I could not argue with.

The third finding was about the reviewers themselves. The isolation probe I had designed planted a marker in the project's always-on memory file and then asserted the marker appeared in no report. Under Claude Code — the one harness the design targeted — that probe is vacuous by construction. The reviewers carry `omitClaudeMd: true`, so the marker lives in a file they never load; the check is guaranteed green whether the mechanism works or the key is silently ignored. Worse, it inspected the one file guaranteed to be excluded and none of the files that were not. A probe that cannot fail is not a probe. It now has a control run in which the exclusion is deliberately disabled and the marker *must* appear.

The fourth was mine. The plan said the always-on layer would shrink because the rules would move into a scoped rules directory. They would not. Claude Code's own documentation says a rule file without `paths` frontmatter "is loaded at launch with the same priority as `.claude/CLAUDE.md`". I had moved bytes between two halves of the same always-on layer and called it a reduction. The file is now 50 lines instead of 166, and the claim is true rather than aspirational, because the rules carry a scope.

## The bloat was smaller than I thought, and worse than I thought

The workspace is 95,762 bytes, which is not much. But four of those findings were not about size at all.

One skill was an orphan. Nothing — no agent, no rule, no document, no script — named it. It was not merely unused: it described a production deployment path that the agent responsible for deploying production refuses to perform. It told an operator that prod builds from whatever is checked out, while the script refuses a prod deploy with neither a tag nor an explicit override. A stale command in a menu is not a cosmetic problem when the person following it is told something false about where their code comes from.

A second skill was 27.5 KB of React, Sonner and Tailwind guidance in a repository whose own agent file says "plain JS only … no framework, no bundler, no npm". The two stack-agnostic skills stayed. The deletion decision did not rest on the framework count at all — it rested on the stack, and it was the review that forced me to say so out loud instead of leaning on a number that would not reproduce.

And then the validator found something no reviewer had named, which is the part I keep coming back to. My project rules still pointed at four files that had been deleted with the old command-line execution layer in v0.29.0: `deploy/kill-mid-task.sh`, `lease_test.go`, `reconcile_test.go`, `smoke_test.go`. Those names sat in the file every session loads. A rule that names a file nobody moved is a rule nobody follows.

## The gate that had to earn its trust

I wrote the validator first and ran it against the pre-change tree, in a clean worktree, where it reported eighteen failures: the orphan skill, the stack-mismatched skill, the four deleted files, and every stale count. Then I applied the sweep and it reported none.

That order matters. A validator that has never failed is not a validator — it is a script that prints green. The number it found is the number that would have shipped quietly, and it is larger than the number I would have found by reading.

It also found two things about itself. My first version flagged its own source file, because the script contained the literal marker prefix it was looking for. And it flagged my own review report, because the report quoted the detector vocabulary — the word "password" — as prose. Both were false positives on my own text, and both are the kind of thing a validator needs to be right about before anyone trusts it: a check that cries wolf on the author's own documentation gets ignored.

## Three problems a website never had

Here is where GAOS stops being a website and starts being a platform, and where the design gate earned its keep.

**The Blueprint loads on every turn.** GAOS generates a workspace per product — a `CLAUDE.md` and a set of rule files — and the agent harness loads them into context on every single turn, including the first one of a brand-new session. So opening a fresh session is not the same as opening a clean one. The reviewer arrives carrying the product's problem statement, its fifty domain rules and its entire decision history, whether or not it asked for them.

That single fact breaks the mechanism the method relies on. On a website, "a fresh context" means a fresh context. Here it means "the same standing files again".

**A retry resumes its session on purpose.** GAOS's own decision log says a retry resumes the session "so the model sees its own failed reply and the review that rejected it". A Goldfish needs the exact opposite: no failed reply, no prior review, no trace of the assumption that started this. The platform's session policy and the method's isolation requirement point in opposite directions, and the platform's rule wins unless something is written down to override it.

**One live turn per workspace.** The rule that keeps two agents from editing the same files also means a review turn and an implementation turn cannot overlap inside one product. The gate is serial by construction, and each review occupies the product's only slot.

I could have shipped a design that papered over all three. Instead the platform half is written down and **built**: two decisions, five business rules, one milestone with five tasks, and seven forward specifications. The mechanism that makes a platform reviewer zero-context is settled — the first of those five tasks builds it rather than deciding it — and the other four wait on that one.

That mechanism is the honest part, and it did not go the way I expected. I had been treating the standing files as the leak. They are not: they are the rubric. The domain rules are rendered into the workspace's rule files, so a reviewer that could not see them could not judge a design against them, and a workspace holding the rubric but not the product's Blueprint is by definition a workspace that has drifted. Cutting the standing files down was never one of the options. It was the wrong question, and reading the code that assembles a packet is what showed me that.

## What the method does not do

It does not find facts. Every reviewer is bound to the document it was given, so a false claim about the repository survives every reviewer and lands in the report as if it were true. Two of the claims in my own first draft were false, and I checked them by hand. Fresh context buys you independence from an author's assumptions, not access to the truth.

It does not replace tests, and it does not replace a human. It is a second pair of eyes with no memory — which is exactly what the first pair of eyes was missing.

It is noisy. The upstream project's author estimates roughly thirty percent of Goldfish suggestions are worth acting on, and my own rounds came in higher than that. Of the fifty-eight raw findings across four lenses, forty-two survived deduplication, and forty-one of those were accepted — one was contested, with a reason, and the rest were applied. That ratio is not a property of the method. It is a property of what I was reviewing, which had real defects in it. What came back as noise instead was wording, a stale anchor, and a reviewer noticing that my own summary was wrong.

And it costs. Five parallel Opus sessions per design, and in this repository nothing bounds that spend — the budget guard that governs the platform's own turns is not built yet. That is a number I am choosing to publish rather than hide, because a gate whose cost nobody measures is a gate nobody can decide about.

## What the repository looks like now

The always-on layer is a quarter of its size and holds four things: the rule that a test is not done until the register says so, the three closed vocabularies, the design-gate trigger, and where to read the rest. The mechanics moved to rule files that load only when you touch a test or an automation. The rubric for a review is the repository's own rules plus the registers, so a reviewer checks a design against the same rules an implementer must satisfy — including the ones that say the event catalog is closed and that the back office currently authenticates nobody.

The last one is worth a line of its own. The platform gate's only privileged act is clearing an open Blocker, which releases the implementation. The surface where that happens is a back office whose entire authentication is a cookie any visitor can obtain by typing something with an `@` in it. A rule saying "only the operator may clear it" is unimplementable until that changes, so the rule now says so, and the milestone carries it as a blocking precondition rather than an assumption.

Five lenses, and the split that earns its cost is not the number but the isolation. Each lens is a separate zero-context session, so one reviewer running five sections would be one session and one author's assumptions applied five times; and a finding is triaged against the lens that raised it, so a lens that was not asked a question cannot raise one.

## Update: the platform question is answered

This note shipped with one thing deliberately left open. The platform half of the gate — the review *inside* GAOS, not the one I ran on this repository — had a question attached to it: how does a platform-run reviewer become genuinely zero-context, and what may the standing files it necessarily contains contain?

It is answered now, and the answer is close to the opposite of the one I was reaching for.

**The isolation boundary is the packet, not the workspace.** A reviewer's packet carries the design and the rubric and nothing else — no brief, no goal, no handoff, no failure history, no answer. Its workspace is the product's own, holding the published Blueprint whole, because that Blueprint *is* the rubric and a reduced set would be drift.

**What actually had to be withheld was my own session state.** A packet is frozen per desk, and the previous stage's handoff is inherited by the next stage at that desk — deliberately, so a workflow's stages chain. A review is a stage at that desk. Left alone, a reviewer would have inherited the author's handoff, the failure lesson drawn from it, and the operator's answer to the author's question: precisely the assumptions the gate exists to strip out. The product's rules travel; the author's process does not.

**The design had to travel.** GAOS reads no folder, so a design that lives in a repository cannot reach a reviewer at all. It is a GAOS-held artifact, carried in the packet and screened on the way out like everything else. The findings travel back the same way, as context, so the implementer genuinely sees the review that rejected it.

**The gate needed nothing new.** It is the planner's own dependency edge and the existing blocked status, so no closed vocabulary widened — the task-status and artifact-kind vocabularies stay closed, they just each gain one value.

The half I still owe is in the note above: the egress pass screens what leaves, not what comes back. A reply is stored verbatim, so a finding that quotes a secret is already in the append-only store by the time the outbound pass refuses the next dispatch. The backstop is not the cure, and inbound screening is separate work.

**Update, later the same day:** the platform half is built now. v0.35.0 ships the review task, the packet that carries the design and the rubric and nothing else, and the gate that makes an implementation wait for its review. Two details I would not have guessed. The gate needs *two* doors closed, not one: the plan is an input to a public method, so a plan that simply omits the review must be refused, and the operator's own dispatch route has to refuse a task whose review has not resolved. And a reviewer's isolation is only as good as the packet — the workspace it runs in is the product's own, standing files and all, because those files are the rubric a reviewer has to judge against.

## Sources

- [Elephant-Goldfish Model](https://github.com/google/elephant-goldfish-model) — Google's open-source plugin for spec-driven design and review
- [Elephants, goldfish, and the new golden age of software engineering](https://medium.com/@drensin/elephants-goldfish-and-the-new-golden-age-of-software-engineering-c33641a48874) — Dave Rensin's essay, the source of the ~30% figure
- [The design gate — GAOS](https://gaos.mfardiansyah.id/docs/architecture/design-gate.html) — this change, in the platform's own documentation

## Read also

- [Elephant-Goldfish Model: a hands-on tutorial, tested on my own agents](/blog/elephant-goldfish-model-tutorial) — the first run, against a design document
- [I ran a goldfish review on my own agent roster, and it found 52 errors my validator never caught](/blog/elephant-goldfish-model-on-a-136-agent-roster) — the same method, one level up, on the agent roster itself
