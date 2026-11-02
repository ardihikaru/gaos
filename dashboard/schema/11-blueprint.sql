-- 11 · The Blueprint, and the gateway's evidence on an attempt.
--
-- D-055..D-062, data model document §15. A product's Blueprint is its
-- standing technical knowledge: policies, knowledge sources, and the immutable
-- versions published from them, which every attempt will pin (BR-39).
--
-- The DRAFT lives in the record document on Product while a human edits it
-- (D-009), beside the BRD; injection materialises it as Policy and
-- KnowledgeSource vertices, exactly as it does Requirement and Issue. A
-- PUBLISHED version is its own vertex from the moment it exists, because it is
-- what a machine reads and what an attempt points at.

CREATE VERTEX TYPE Policy IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY Policy.productId STRING  (mandatory true, notnull true);
CREATE PROPERTY Policy.key       STRING  (mandatory true, notnull true);
CREATE PROPERTY Policy.kind      STRING  (mandatory true, notnull true,
                 regexp 'technical|coding|security|testing|custom');
CREATE PROPERTY Policy.title     STRING  (notnull true);
CREATE PROPERTY Policy.body      STRING;
CREATE PROPERTY Policy.order     INTEGER (min 0);
CREATE PROPERTY Policy.scope     STRING;                -- a file glob; unused in v1

CREATE VERTEX TYPE KnowledgeSource IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY KnowledgeSource.productId STRING (mandatory true, notnull true);
CREATE PROPERTY KnowledgeSource.kind      STRING (mandatory true, regexp 'git-repo');
CREATE PROPERTY KnowledgeSource.url       STRING (mandatory true, notnull true);  -- https, no credentials
CREATE PROPERTY KnowledgeSource.ref       STRING (mandatory true, notnull true);  -- tag or SHA, never a branch
CREATE PROPERTY KnowledgeSource.purpose   STRING (regexp 'style|structure|interfaces|reference');
CREATE PROPERTY KnowledgeSource.note      STRING;

-- files is [{Path, SHA256, Bytes, Content}]: the rendered workspace files, so a
-- version can be synced to a workspace long after the draft moved on.
CREATE VERTEX TYPE BlueprintVersion IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY BlueprintVersion.productId   STRING   (mandatory true, notnull true);
CREATE PROPERTY BlueprintVersion.n           INTEGER  (mandatory true, min 1);
CREATE PROPERTY BlueprintVersion.digest      STRING   (mandatory true, notnull true);
CREATE PROPERTY BlueprintVersion.files       LIST;
CREATE PROPERTY BlueprintVersion.publishedAt DATETIME (mandatory true);

CREATE EDGE TYPE HAS_POLICY            IF NOT EXISTS EXTENDS GaosEdge;  -- Product -> Policy
CREATE EDGE TYPE HAS_KNOWLEDGE_SOURCE  IF NOT EXISTS EXTENDS GaosEdge;  -- Product -> KnowledgeSource
CREATE EDGE TYPE HAS_BLUEPRINT_VERSION IF NOT EXISTS EXTENDS GaosEdge;  -- Product -> BlueprintVersion
CREATE EDGE TYPE PINNED_TO             IF NOT EXISTS EXTENDS GaosEdge;  -- TaskRun -> BlueprintVersion (M14.4)
CREATE EDGE TYPE HOSTED_IN             IF NOT EXISTS EXTENDS GaosEdge;  -- Product -> Workspace (M14.4)

-- D-047: an identity index on every type the store addresses by business id.
CREATE INDEX IF NOT EXISTS ON Policy           (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON KnowledgeSource  (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON BlueprintVersion (tenantId, id) UNIQUE;
-- One digest names one version of one product: an unchanged publish is a no-op.
CREATE INDEX IF NOT EXISTS ON BlueprintVersion (tenantId, productId, digest) UNIQUE;

-- D-045: edge uniqueness is an index on (`@out`, `@in`), not a keyword.
CREATE INDEX IF NOT EXISTS ON HAS_POLICY            (`@out`, `@in`) UNIQUE;
CREATE INDEX IF NOT EXISTS ON HAS_KNOWLEDGE_SOURCE  (`@out`, `@in`) UNIQUE;
CREATE INDEX IF NOT EXISTS ON HAS_BLUEPRINT_VERSION (`@out`, `@in`) UNIQUE;
CREATE INDEX IF NOT EXISTS ON PINNED_TO             (`@out`, `@in`) UNIQUE;
CREATE INDEX IF NOT EXISTS ON HOSTED_IN             (`@out`, `@in`) UNIQUE;

-- The gateway's evidence on an attempt (D-060, D-062). The request id is what
-- recovery polls after a restart, so it is unique per tenant.
CREATE PROPERTY TaskRun.requestId        STRING;
CREATE PROPERTY TaskRun.sessionId        STRING;
CREATE PROPERTY TaskRun.blueprintVersion INTEGER (min 1);
CREATE INDEX IF NOT EXISTS ON TaskRun (tenantId, requestId) NOTUNIQUE;

-- D-062: a turn through ai-manager records invocation = gateway. Widened in
-- place, because 04-execution.sql's CREATE PROPERTY cannot change an existing
-- property's constraint on a database that already has it.
ALTER PROPERTY TaskRun.invocation REGEXP 'cli|api|gateway';

-- A fix found while proving this file against a scratch 26.7.2: T-064 stores
-- every attempt's handoff as an Artifact of kind 'handoff', and 02-master.sql's
-- regexp never allowed it -- so on ArcadeDB the write was refused at commit and
-- the handoff never reached the graph ("background write did not reach the
-- graph"). TestAnAttemptSurvivesARestart fails on the scratch database without
-- this line. Widened in place, for the same reason as TaskRun.invocation above.
-- M14.5 adds 'prompt' and 'reply': a gateway turn's transcript, stored per attempt
-- because the session that carried it expires (BR-41).
ALTER PROPERTY Artifact.kind REGEXP 'sequence|erd|flow|diff|commit|report|log|upload|handoff|prompt|reply';
