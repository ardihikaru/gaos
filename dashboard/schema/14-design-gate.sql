-- 14-design-gate.sql — M17, the design gate (D-069). A task's kind says which reply
-- contract it is asked for, what its packet carries, and what the gate does with
-- it. The gate itself is the planner's own DEPENDS_ON edge, so nothing here
-- widens a task status, a role, or the event catalog.
--
-- Applied last: schemaFiles() sorts by filename (schema_embed.go:116-132), and
-- every statement below is an ALTER of a type an earlier file created.

-- The task kind. Three values, closed.
--
-- Not mandatory and not defaulted on purpose: this is a property added to a
-- type that already exists in deployed databases, and the precedent for that is
-- Task.rank (11-blueprint.sql:63) and TaskRun.provider (13-vendors.sql:15) --
-- both optional, both with a regexp, neither mandatory. A task written before
-- this migration has no kind, and the Go side reads that as 'implement'.
--
-- The regexp is ANCHORED, unlike its siblings, because this one is a security
-- boundary: an unanchored alternation would admit 'review-x', and the Go side
-- refuses a kind outside the vocabulary. Anchoring means the database and the
-- Go side cannot disagree about what the three names are.
CREATE PROPERTY Task.kind IF NOT EXISTS STRING (regexp '^(implement|design|review)$');

-- A design artifact, and the gate's own record.
--
-- Both are artifacts rather than Reviews: the Review vertex belongs to the M5
-- score-band engine, whose verdict vocabulary is closed at six values and
-- whose finalScore is mandatory (06-judgment.sql:14-26). A design gate has no
-- score to record and no band to fall in, so D-069 clause 8 gives it an
-- Artifact instead and leaves the Review vertex to the engine that owns it.
--
-- The list is the whole vocabulary, not an append: ArcadeDB's ALTER PROPERTY
-- replaces the regexp, so a shortened list would refuse every existing row.
-- 02-master.sql's CREATE is widened to the same list, and
-- TestTheQuestionDDLWidensWhatTheCreateDeclares (questions_test.go:247-262)
-- is what keeps the two in agreement.
ALTER PROPERTY Artifact.kind REGEXP 'sequence|erd|flow|diff|commit|report|log|upload|handoff|prompt|reply|question|design|review';

-- The design artifact is cited by the part that carries it, so the audit edge
-- can say why the reviewer was given it. The list is the whole vocabulary
-- again, for the same reason.
ALTER PROPERTY USES_CONTEXT.role REGEXP 'requirement|rule|boundary|story|criterion|diagram|decision|lesson|code|diff|policy|design';
