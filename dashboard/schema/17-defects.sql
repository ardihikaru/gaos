-- 17 · Defect triage and auto-fix (D-091; docs/designs/gaos-defect-autofix.md §5.1, §9.1).
--
-- A defect the QA tool reported, one vertex per key, and the draft pull request
-- an easy one produced. Neither is part of the floor and neither is rebuilt from
-- the event stream: both are written in the transaction of the change and read
-- once at boot. `excerpt` is the untrusted failure text; a retention job empties
-- it 30 days after `lastSeen`. `context` is the packet's source slice (at most
-- 96 KiB); it is never in an event.
--
-- The vertex id of a Defect is its key, "<productId>/<defectId or caseId>".

CREATE VERTEX TYPE Defect IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY Defect.productId       STRING  (mandatory true, notnull true);
CREATE PROPERTY Defect.defectId        STRING;
CREATE PROPERTY Defect.caseId          STRING  (mandatory true, notnull true);
CREATE PROPERTY Defect.kind            STRING  (mandatory true, regexp 'xfail|fail|xpass');
CREATE PROPERTY Defect.level           STRING;
CREATE PROPERTY Defect.automationType  STRING  (mandatory true, regexp 'be|e2e');
CREATE PROPERTY Defect.backend         STRING;
CREATE PROPERTY Defect.targetRepos     STRING;   -- a JSON array, the owned ones only
CREATE PROPERTY Defect.operations      STRING;   -- a JSON array, search terms only
CREATE PROPERTY Defect.spec            STRING;
CREATE PROPERTY Defect.title           STRING;
CREATE PROPERTY Defect.excerpt         STRING;
CREATE PROPERTY Defect.excerptWithheld BOOLEAN (default false);
CREATE PROPERTY Defect.firstSeen       LONG    (mandatory true);
CREATE PROPERTY Defect.lastSeen        LONG    (mandatory true);
CREATE PROPERTY Defect.runRef          STRING;
CREATE PROPERTY Defect.gen             INTEGER (mandatory true, min 1);
CREATE PROPERTY Defect.regression      BOOLEAN (default false);
CREATE PROPERTY Defect.difficulty      STRING  (regexp 'easy|medium|complex');
CREATE PROPERTY Defect.triageClass      STRING;   -- the verdict class (not named `class`: a reserved word in the SQL dialect)
CREATE PROPERTY Defect.repo            STRING;   -- the verdict's repository
CREATE PROPERTY Defect.files           STRING;   -- a JSON array, the verdict's files
CREATE PROPERTY Defect.reason          STRING;
CREATE PROPERTY Defect.state           STRING  (mandatory true, regexp 'open|triaged|fixing|fixed|closed');
CREATE PROPERTY Defect.supersededBy    STRING;
CREATE PROPERTY Defect.triageTask      STRING;
CREATE PROPERTY Defect.fixTask         STRING;
CREATE PROPERTY Defect.fixPR           STRING;
CREATE PROPERTY Defect.context         STRING;   -- the packet's source slice, at most 96 KiB

CREATE VERTEX TYPE FixPR IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY FixPR.productId  STRING  (mandatory true, notnull true);
CREATE PROPERTY FixPR.defectKey  STRING  (mandatory true, notnull true);
CREATE PROPERTY FixPR.repo       STRING  (mandatory true, notnull true);
CREATE PROPERTY FixPR.number     INTEGER;
CREATE PROPERTY FixPR.url        STRING;
CREATE PROPERTY FixPR.state      STRING  (mandatory true, regexp 'preparing|draft|ready|merged|closed');
-- openedAt and changedAt are epoch seconds. They are not createdAt/updatedAt: those
-- are GaosVertex's own DATETIME properties and ArcadeDB refuses to redeclare them.
CREATE PROPERTY FixPR.openedAt   LONG    (mandatory true);
CREATE PROPERTY FixPR.changedAt  LONG    (mandatory true);
CREATE PROPERTY FixPR.taskId     STRING;
CREATE PROPERTY FixPR.difficulty STRING;
CREATE PROPERTY FixPR.beforeText     STRING;
CREATE PROPERTY FixPR.afterText      STRING;
CREATE PROPERTY FixPR.branch     STRING;
CREATE PROPERTY FixPR.baseSHA    STRING;
CREATE PROPERTY FixPR.commitSHA  STRING;
CREATE PROPERTY FixPR.waSent     BOOLEAN (default false);
CREATE PROPERTY FixPR.reply      STRING;   -- the stored parsed reply, so a restart pays for no second turn

-- D-092 (docs/designs/gaos-pr-overview.md §3.2). baseName is the branch the pull request targets
-- ("base" is an SQL keyword); retargetedFrom/To link a pull request to the one it replaced or was
-- replaced by; baseOverride is the base a retarget set for the defect's next fix (cleared again).
CREATE PROPERTY FixPR.baseName       STRING;
CREATE PROPERTY FixPR.retargetedFrom STRING;
CREATE PROPERTY FixPR.retargetedTo   STRING;
CREATE PROPERTY Defect.baseOverride  STRING;

CREATE INDEX IF NOT EXISTS ON Defect (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON FixPR (tenantId, id) UNIQUE;

-- The role vocabulary on a database that already exists. CREATE PROPERTY above
-- (04-execution.sql, 05-capacity.sql) keeps the regexp it first declared, so a
-- deployment older than v0.48.0 would refuse a task or an agent of a newer role
-- at commit. ALTER is idempotent and applies to every deployment.
ALTER PROPERTY Task.role  REGEXP 'planner|architect|backend|frontend|qa|security|devops|pr-reviewer|unit-test|triager|fixer';
ALTER PROPERTY Agent.role REGEXP 'planner|architect|backend|frontend|qa|security|devops|pr-reviewer|unit-test|triager|fixer';
