-- 15-reviewer-workspace.sql — M17.5, the reviewer's own workspace (D-071).
--
-- A review task's turn used to run in the product's own workspace — the folder
-- the author's turn writes into, and the key the author's transcript is stored
-- under. The product now has a second workspace, its reviewer workspace, and
-- the run record says which one the turn used: the drift check after the turn
-- must examine the folder the run touched, or a reviewer's turn marks the
-- author's workspace drifted and refuses the author's next dispatch.
--
-- A free string, not a closed vocabulary: it is a workspace id, and "" means
-- the run was recorded before M17.5, which ran in the product's own workspace.

CREATE PROPERTY TaskRun.workspace IF NOT EXISTS STRING;
