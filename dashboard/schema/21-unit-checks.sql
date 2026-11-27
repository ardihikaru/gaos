-- 21 - Unit checks (D-105; docs/designs/release-b4-unit-tests.md §7.1, §12).
--
-- A failing unit-test check of a fix-target repository's merge commit becomes a Defect of kind
-- "unit". unitBase is the merged base, unitSha the merge commit and unitMergedAt its merged_at;
-- unitTests the kept failing tests ("<test>\t<file>" per line, emptied by the 30-day retention)
-- and unitFiles the distinct failing files a unit fix may not edit (a JSON list, kept).
-- unitReopenedAt and unitPassedAt are epoch seconds (0 = not); the two booleans say whether
-- the held and the passed notices were sent. ALTER is idempotent.
ALTER PROPERTY Defect.kind REGEXP 'xfail|fail|xpass|review|unit';
CREATE PROPERTY Defect.unitSha            STRING;
CREATE PROPERTY Defect.unitBase           STRING;
CREATE PROPERTY Defect.unitTests          STRING;
CREATE PROPERTY Defect.unitFiles          STRING;
CREATE PROPERTY Defect.unitMergedAt       LONG;
CREATE PROPERTY Defect.unitReopenedAt     LONG;
CREATE PROPERTY Defect.unitPassedAt       LONG;
CREATE PROPERTY Defect.unitNoticeSent     BOOLEAN;
CREATE PROPERTY Defect.unitPassNoticeSent BOOLEAN;

-- One vertex per merge commit whose check GAOS awaits (a watch). Outside the event floor like
-- Defect and PullRequest: written in the transaction of the change, read once at boot. The
-- vertex id is "<productId>/<owner>/<repo>@<sha>", lower-cased (a watch whose merge could not be
-- read is "<productId>/<owner>/<repo>#<pr>" and carries forty zeros as its sha). Times are epoch
-- seconds.
CREATE VERTEX TYPE UnitWatch IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY UnitWatch.productId IF NOT EXISTS STRING (mandatory true, notnull true);
CREATE PROPERTY UnitWatch.repo      IF NOT EXISTS STRING (mandatory true, notnull true);
CREATE PROPERTY UnitWatch.pr        IF NOT EXISTS INTEGER;
CREATE PROPERTY UnitWatch.sha       IF NOT EXISTS STRING (mandatory true, notnull true, regexp '[0-9a-f]{40}');
CREATE PROPERTY UnitWatch.base      IF NOT EXISTS STRING (mandatory true, notnull true);
CREATE PROPERTY UnitWatch.mergedAt  IF NOT EXISTS LONG;
CREATE PROPERTY UnitWatch.firstSeen IF NOT EXISTS LONG;
CREATE PROPERTY UnitWatch.nextAt    IF NOT EXISTS LONG;
CREATE PROPERTY UnitWatch.doneAt    IF NOT EXISTS LONG;
CREATE PROPERTY UnitWatch.tries     IF NOT EXISTS INTEGER;
CREATE PROPERTY UnitWatch.reads     IF NOT EXISTS INTEGER;
CREATE PROPERTY UnitWatch.state     IF NOT EXISTS STRING (mandatory true, notnull true, regexp 'pending|done');
CREATE PROPERTY UnitWatch.outcome   IF NOT EXISTS STRING (regexp '|passed|failed|no-ci|inconclusive|dedup|updated|refused:[a-z-]+|skipped:[a-z-]+');
CREATE PROPERTY UnitWatch.defectKey IF NOT EXISTS STRING;
CREATE INDEX IF NOT EXISTS ON UnitWatch (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON UnitWatch (tenantId, productId, state) NOTUNIQUE;
