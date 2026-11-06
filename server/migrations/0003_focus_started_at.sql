-- D4 (2026-11-06): the D1 presence mirror keeps the focus start too, so REST reads that take presence from D1
-- (serializeMe: signup, and serializeMeLive's fallback when the UserHub is unreachable) report the exact
-- Focus.started_at instead of a stand-in. Written by UserHub.writePresence together with focus_ends_at:
-- both non-NULL while a focus is active, both NULL otherwise.
ALTER TABLE user_presence ADD COLUMN focus_started_at INTEGER;
