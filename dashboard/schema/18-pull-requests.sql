-- 18 · The Pull requests page (D-092; docs/designs/gaos-pr-overview.md §3.2).
--
-- One vertex per review or QA-tool pull request of a product: a cache of GitHub's state, outside
-- the event floor like Defect and FixPR. Fix pull requests stay in FixPR. The vertex id is
-- "<productId>/<owner>/<repo>#<n>", lower-cased. No url is stored: every link is rebuilt from the
-- validated owner/repo and number. openedAt/checkedAt are epoch seconds (createdAt and
-- updatedAt are GaosVertex's own).

CREATE VERTEX TYPE PullRequest IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY PullRequest.productId STRING  (mandatory true, notnull true);
CREATE PROPERTY PullRequest.repo      STRING  (mandatory true, notnull true);
CREATE PROPERTY PullRequest.number    INTEGER (mandatory true, notnull true);
CREATE PROPERTY PullRequest.kind      STRING  (mandatory true, regexp 'review|qa');
CREATE PROPERTY PullRequest.state     STRING  (mandatory true, regexp 'open|draft|merged|closed');
CREATE PROPERTY PullRequest.title     STRING;
CREATE PROPERTY PullRequest.author    STRING;
CREATE PROPERTY PullRequest.headRef   STRING;
CREATE PROPERTY PullRequest.baseRef   STRING;
CREATE PROPERTY PullRequest.taskId    STRING;
CREATE PROPERTY PullRequest.qaJob     STRING;
CREATE PROPERTY PullRequest.qaCase    STRING;
CREATE PROPERTY PullRequest.qaLink    STRING;
CREATE PROPERTY PullRequest.openedAt  LONG;
CREATE PROPERTY PullRequest.checkedAt LONG;

CREATE INDEX IF NOT EXISTS ON PullRequest (tenantId, id) UNIQUE;
