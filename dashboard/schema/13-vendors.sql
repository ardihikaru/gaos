-- 13-vendors.sql — M16, a vendor policy for each product (D-067, BR-45).
--
-- A product's tasks run on Claude CLI, on the Antigravity pool, or on both by
-- role. The policy itself rides on the Product vertex as a document, beside
-- the Blueprint draft (D-009: schema-less where humans write), so it needs no
-- DDL. What a machine reads is the vendor each attempt went to: a retry whose
-- vendor changed opens a new session (SessionOpened reason 'vendor'), and an
-- auditor asks which vendor ran which attempt. A run recorded before M16 has
-- no provider and went to Claude, the only vendor there was.

CREATE PROPERTY TaskRun.provider IF NOT EXISTS STRING (regexp 'claude|antigravity');
