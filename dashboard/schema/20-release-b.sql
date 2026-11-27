-- 20 - Release B (D-103; docs/designs/release-b-e2e-kinds-merge-unit-tests.md §4.3).
--
-- A QA spec pull request's row keeps the commit the QA tool pushed after a person approved the
-- spec (qaSha), a note when GAOS did not run its case after the merge (qaNote), and when the row
-- became merged or closed (finalAt, epoch seconds). Additive and idempotent.
CREATE PROPERTY PullRequest.qaSha   STRING;
CREATE PROPERTY PullRequest.qaNote  STRING;
CREATE PROPERTY PullRequest.finalAt LONG;
