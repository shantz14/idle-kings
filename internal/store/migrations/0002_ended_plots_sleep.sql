-- Ended plots used to keep their last next_step_at, so DueConspiracies
-- rescanned every one of them on every tick. Park them at the far future
-- whenever their activity resolves, however it resolves.
UPDATE conspiracies SET next_step_at = 9223372036854775807
 WHERE activity_id IN (SELECT id FROM activities WHERE resolved_at IS NOT NULL);

CREATE TRIGGER IF NOT EXISTS conspiracies_sleep_when_resolved
AFTER UPDATE OF resolved_at ON activities
WHEN NEW.resolved_at IS NOT NULL AND NEW.kind = 'conspiracy'
BEGIN
    UPDATE conspiracies SET next_step_at = 9223372036854775807 WHERE activity_id = NEW.id;
END;
