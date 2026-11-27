-- 22 - Unit results from Jenkins (D-106; docs/designs/release-b4-unit-tests.md §5.4).
-- runUrl is the Jenkins build URL a posted result carried (https, at most 512 bytes, screened, else empty);
-- empty for a GitHub Actions result. Shown on the Auto-fix page, never sent to an agent.
-- postedFrom is the caller's address as the gate keyed it; postedResult the result it posted
-- (passed | failed | build-failed). Both empty for a GitHub Actions result.
-- conflictNoticeSent: the one "conflicting unit result" notice of this watch was sent.
CREATE PROPERTY UnitWatch.runUrl             IF NOT EXISTS STRING;
CREATE PROPERTY UnitWatch.postedFrom         IF NOT EXISTS STRING;
CREATE PROPERTY UnitWatch.postedResult       IF NOT EXISTS STRING (regexp '|passed|failed|build-failed');
CREATE PROPERTY UnitWatch.conflictNoticeSent IF NOT EXISTS BOOLEAN;
