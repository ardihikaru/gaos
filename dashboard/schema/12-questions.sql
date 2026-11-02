-- 12-questions.sql — M15, "Needs you" (D-064).
--
-- An agent may end its turn with a question for the operator: three options,
-- the first recommended, and a free-text fourth. The task then waits in
-- AWAITING_INPUT until the operator answers (BR-44), and the answer goes back
-- as the next turn of the same session.
--
-- Both statements widen a constraint in place, the 11-blueprint.sql way.
-- 04-execution.sql now declares the wider Task.status for a new database, but
-- its CREATE PROPERTY cannot change a property that already exists, so a
-- database created before M15 needs the ALTER; Artifact.kind has only ever been
-- widened by ALTER (11-blueprint.sql), and this is the next widening. Re-running it is
-- the same assignment twice, so the file is idempotent without IF NOT EXISTS.
-- TestTheQuestionDDLWidensWhatTheCreateDeclares keeps the two copies equal.

ALTER PROPERTY Task.status REGEXP 'PENDING|READY|DISPATCHED|RUNNING|REVIEW|BLOCKED|DEFERRED|DONE|FAILED|CANCELED|AWAITING_INPUT';

-- 'question' is the agent's question and, once answered, the answer: one
-- artifact per asking attempt, so the audit trail outlives the 30-day bucket.
ALTER PROPERTY Artifact.kind REGEXP 'sequence|erd|flow|diff|commit|report|log|upload|handoff|prompt|reply|question';
