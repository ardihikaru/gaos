-- 19 · Review defects (D-099; docs/designs/agent-seats-and-pr-agents.md §6.2).
--
-- A low PR-review finding becomes a Defect of kind "review", fixed through the
-- D-091 auto-fix path into the reviewed pull request's own branch. reviewBranch
-- is that branch (head.ref) and reviewHead the commit the reviewer read: the
-- pinned head, the parent of the fix commit. A review defect leaves
-- baseOverride, the retarget's field, empty. ALTER is idempotent.
ALTER PROPERTY Defect.kind REGEXP 'xfail|fail|xpass|review';
CREATE PROPERTY Defect.reviewBranch STRING;
CREATE PROPERTY Defect.reviewHead   STRING;
