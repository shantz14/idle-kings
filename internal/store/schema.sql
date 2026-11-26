-- One court per Discord server.
CREATE TABLE IF NOT EXISTS courts (
    guild_id                 TEXT PRIMARY KEY,
    admin_user_id            TEXT NOT NULL,
    announcements_channel_id TEXT,                          -- public actions post here; set on /start
    status                   TEXT NOT NULL DEFAULT 'setup', -- setup | running
    created_at               INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS characters (
    id             INTEGER PRIMARY KEY,
    guild_id       TEXT NOT NULL REFERENCES courts(guild_id),
    user_id        TEXT NOT NULL,
    name           TEXT NOT NULL,
    family_name    TEXT NOT NULL,
    role           TEXT NOT NULL,
    stats          TEXT NOT NULL,           -- JSON object, so stats can change without migrations
    background     TEXT NOT NULL DEFAULT '',
    crest          TEXT NOT NULL DEFAULT '{}', -- JSON game.Crest
    influence      REAL NOT NULL DEFAULT 0,
    renown         REAL NOT NULL DEFAULT 0,
    wealth         INTEGER NOT NULL DEFAULT 0,
    daily_income   INTEGER NOT NULL DEFAULT 0, -- base income; estate items add on top
    last_income_at INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    UNIQUE (guild_id, user_id)
);

-- What each character owns: their home and possessions (keys into game.Items).
-- Estate value is derived from these.
CREATE TABLE IF NOT EXISTS estate_items (
    id           INTEGER PRIMARY KEY,
    character_id INTEGER NOT NULL REFERENCES characters(id),
    item_key     TEXT NOT NULL,
    acquired_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS estate_items_owner ON estate_items (character_id);

-- Character creator progress, so a closed popup can be resumed.
CREATE TABLE IF NOT EXISTS character_drafts (
    guild_id   TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    data       TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (guild_id, user_id)
);

-- A timed action, e.g. a party. Effects apply when it resolves.
CREATE TABLE IF NOT EXISTS activities (
    id                  INTEGER PRIMARY KEY,
    guild_id            TEXT NOT NULL,
    host_id             INTEGER NOT NULL REFERENCES characters(id),
    kind                TEXT NOT NULL,
    goal                TEXT NOT NULL DEFAULT '',
    spend               INTEGER NOT NULL DEFAULT 0, -- gold the host paid
    public              INTEGER NOT NULL DEFAULT 0,
    announce_channel_id TEXT,
    announce_message_id TEXT,
    started_at          INTEGER NOT NULL,
    ends_at             INTEGER NOT NULL,
    resolved_at         INTEGER
);
CREATE INDEX IF NOT EXISTS activities_open ON activities (ends_at) WHERE resolved_at IS NULL;

-- Other players joining a public activity.
CREATE TABLE IF NOT EXISTS participants (
    activity_id  INTEGER NOT NULL REFERENCES activities(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    goal         TEXT NOT NULL,
    target_id    INTEGER REFERENCES characters(id),
    joined_at    INTEGER NOT NULL,
    PRIMARY KEY (activity_id, character_id)
);

-- Prompts sent to a player during an activity (e.g. "out of wine").
CREATE TABLE IF NOT EXISTS activity_events (
    id            INTEGER PRIMARY KEY,
    activity_id   INTEGER NOT NULL REFERENCES activities(id),
    character_id  INTEGER NOT NULL REFERENCES characters(id),
    event_key     TEXT NOT NULL,
    fires_at      INTEGER NOT NULL,
    deadline_at   INTEGER NOT NULL,
    sent_at       INTEGER,
    dm_channel_id TEXT,
    dm_message_id TEXT,
    choice        TEXT,
    result        TEXT,  -- JSON game.EventOutcome
    resolved_at   INTEGER
);
CREATE INDEX IF NOT EXISTS events_open ON activity_events (fires_at) WHERE resolved_at IS NULL;

-- ---- the Senate ----

-- Realm-wide trackers and the daily matter schedule, one per court.
CREATE TABLE IF NOT EXISTS realms (
    guild_id       TEXT PRIMARY KEY REFERENCES courts(guild_id),
    trackers       TEXT NOT NULL,                 -- JSON game.Realm
    status         TEXT NOT NULL DEFAULT 'alive', -- alive | fallen
    fallen_reason  TEXT,
    next_matter_at INTEGER NOT NULL,
    recent         TEXT NOT NULL DEFAULT '[]'     -- JSON list of recent event keys
);

CREATE TABLE IF NOT EXISTS senators (
    id          INTEGER PRIMARY KEY,
    guild_id    TEXT NOT NULL REFERENCES courts(guild_id),
    name        TEXT NOT NULL,
    traits      TEXT NOT NULL,  -- JSON list
    influence   REAL NOT NULL,
    base_price  INTEGER NOT NULL
);

-- A daily matter put to a vote.
CREATE TABLE IF NOT EXISTS matters (
    id          INTEGER PRIMARY KEY,
    guild_id    TEXT NOT NULL REFERENCES courts(guild_id),
    event_key   TEXT NOT NULL,
    opened_at   INTEGER NOT NULL,
    closes_at   INTEGER NOT NULL,
    resolved_at INTEGER,
    channel_id  TEXT,
    message_id  TEXT,
    result      TEXT   -- JSON summary once decided
);
CREATE INDEX IF NOT EXISTS matters_open ON matters (closes_at) WHERE resolved_at IS NULL;

CREATE TABLE IF NOT EXISTS votes (
    matter_id    INTEGER NOT NULL REFERENCES matters(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    option       TEXT NOT NULL,
    voted_at     INTEGER NOT NULL,
    locked       INTEGER NOT NULL DEFAULT 0, -- 1 once sold; can't be changed
    sold_to      TEXT,                       -- who bought it, for the record
    PRIMARY KEY (matter_id, character_id)
);

-- Player-to-player vote deals. kind 'sell': from_id offers their vote to
-- to_id, who picks the option on accepting. kind 'buy': from_id offers to
-- pay to_id to vote for option.
CREATE TABLE IF NOT EXISTS vote_offers (
    id         INTEGER PRIMARY KEY,
    matter_id  INTEGER NOT NULL REFERENCES matters(id),
    kind       TEXT NOT NULL,
    from_id    INTEGER NOT NULL REFERENCES characters(id),
    to_id      INTEGER NOT NULL REFERENCES characters(id),
    option     TEXT,
    price      INTEGER NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | declined
    created_at INTEGER NOT NULL
);

-- A senator can be bought once per matter.
CREATE TABLE IF NOT EXISTS bribes (
    matter_id    INTEGER NOT NULL REFERENCES matters(id),
    senator_id   INTEGER NOT NULL REFERENCES senators(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    option       TEXT NOT NULL,
    amount       INTEGER NOT NULL,
    paid_at      INTEGER NOT NULL,
    PRIMARY KEY (matter_id, senator_id)
);

-- Follow-up events waiting their turn.
CREATE TABLE IF NOT EXISTS realm_queue (
    id        INTEGER PRIMARY KEY,
    guild_id  TEXT NOT NULL REFERENCES courts(guild_id),
    event_key TEXT NOT NULL,
    due_at    INTEGER NOT NULL
);
