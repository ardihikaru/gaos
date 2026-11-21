-- 16 · The QA catalog (D-072, D-073; docs/designs/qa-catalog-sync.md §4.3).
--
-- A product's test cases, one vertex each. The planning half (what the case is,
-- what is decided about it) is written by GAOS; the automation half
-- (implemented, spec, result, runAt, runRef) by the QA tools. The catalog is
-- machine-read, so it is schema-full here; the product's modules, epics, head
-- and sync state ride on the Product record as the document `qaState`
-- (D-009, like vendorPolicy) and need no DDL.
--
-- The vertex id is "<productId>:<caseId>" (D-047). `result` is constrained, so
-- an empty one is stored as an ABSENT property, never as ''.

CREATE VERTEX TYPE QaCase IF NOT EXISTS EXTENDS GaosVertex;
CREATE PROPERTY QaCase.productId   STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.caseId      STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.epic        STRING  (mandatory true, notnull true);
CREATE PROPERTY QaCase.module      STRING;
CREATE PROPERTY QaCase.features    LIST;
CREATE PROPERTY QaCase.userStory   STRING;
CREATE PROPERTY QaCase.scenario    STRING  (notnull true);
CREATE PROPERTY QaCase.polarity    STRING  (mandatory true);
CREATE PROPERTY QaCase.level       STRING  (mandatory true, regexp 'API|GQL|WEB|E2E');
CREATE PROPERTY QaCase.pri         STRING  (mandatory true, regexp 'P0|P1|P2|P3');
CREATE PROPERTY QaCase.decision    STRING  (mandatory true, regexp 'planned|blocked|manual-only|wont');
CREATE PROPERTY QaCase.reason      STRING;
CREATE PROPERTY QaCase.implemented BOOLEAN;
CREATE PROPERTY QaCase.spec        STRING;
CREATE PROPERTY QaCase.result      STRING  (regexp 'pass|fail|skipped');
CREATE PROPERTY QaCase.runAt       LONG;
CREATE PROPERTY QaCase.runRef      STRING;
CREATE PROPERTY QaCase.catRev      INTEGER (mandatory true, min 0);

CREATE EDGE TYPE HAS_QA_CASE IF NOT EXISTS EXTENDS GaosEdge;   -- Product -> QaCase

CREATE INDEX IF NOT EXISTS ON QaCase (tenantId, id) UNIQUE;
CREATE INDEX IF NOT EXISTS ON QaCase (tenantId, productId, catRev) NOTUNIQUE;
CREATE INDEX IF NOT EXISTS ON HAS_QA_CASE (`@out`, `@in`) UNIQUE;
