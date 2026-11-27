-- One court per Discord server.
CREATE TABLE IF NOT EXISTS courts (
    guild_id                 TEXT PRIMARY KEY,
    admin_user_id            TEXT NOT NULL,
    announcements_channel_id TEXT,                          -- public actions post here; set on /start
    status                   TEXT NOT NULL DEFAULT 'setup', -- setup | running | fallen
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
    residence_id   INTEGER,                    -- estate_items.id of the property they live in
    heir_unnamed   INTEGER NOT NULL DEFAULT 0, -- 1 after a death, until the player names the heir
    last_income_at INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    UNIQUE (guild_id, user_id)
);
CREATE INDEX IF NOT EXISTS characters_user ON characters (user_id);
CREATE INDEX IF NOT EXISTS characters_income ON characters (last_income_at);

-- What each character owns: properties and possessions (keys into game.Items).
-- Estate value is derived from these. Unique items have one row per court.
CREATE TABLE IF NOT EXISTS estate_items (
    id           INTEGER PRIMARY KEY,
    character_id INTEGER NOT NULL REFERENCES characters(id),
    guild_id     TEXT NOT NULL,
    item_key     TEXT NOT NULL,
    price_paid   INTEGER NOT NULL DEFAULT 0,
    acquired_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS estate_items_owner ON estate_items (character_id);
CREATE INDEX IF NOT EXISTS estate_items_court ON estate_items (guild_id, item_key);

-- Estate deals between players: give = the sender's estate_items ids,
-- want = the recipient's; gold > 0 is paid by the recipient to the sender,
-- gold < 0 by the sender to the recipient.
CREATE TABLE IF NOT EXISTS estate_offers (
    id         INTEGER PRIMARY KEY,
    guild_id   TEXT NOT NULL,
    from_id    INTEGER NOT NULL REFERENCES characters(id),
    to_id      INTEGER NOT NULL REFERENCES characters(id),
    give       TEXT NOT NULL DEFAULT '[]',
    want       TEXT NOT NULL DEFAULT '[]',
    gold       INTEGER NOT NULL DEFAULT 0,
    status     TEXT NOT NULL DEFAULT 'pending', -- pending | accepted | declined | cancelled
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS estate_offers_open ON estate_offers (status, from_id, to_id);
CREATE INDEX IF NOT EXISTS estate_offers_guild ON estate_offers (guild_id) WHERE status = 'pending';

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
    target_id           INTEGER NOT NULL DEFAULT 0, -- kind-specific target, e.g. the senator being courted
    public              INTEGER NOT NULL DEFAULT 0,
    announce_channel_id TEXT,
    announce_message_id TEXT,
    started_at          INTEGER NOT NULL,
    ends_at             INTEGER NOT NULL,
    resolved_at         INTEGER
);
CREATE INDEX IF NOT EXISTS activities_open ON activities (ends_at) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS activities_host ON activities (host_id) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS activities_target ON activities (target_id, kind) WHERE resolved_at IS NULL;

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
CREATE INDEX IF NOT EXISTS events_deadline ON activity_events (deadline_at) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS events_activity ON activity_events (activity_id);

-- ---- the Senate ----

-- Realm-wide trackers and the daily matter schedule, one per court.
CREATE TABLE IF NOT EXISTS realms (
    guild_id       TEXT PRIMARY KEY REFERENCES courts(guild_id),
    trackers       TEXT NOT NULL,                 -- JSON game.Realm
    sectors        TEXT NOT NULL DEFAULT '{}',    -- JSON game.SectorState
    laws           TEXT NOT NULL DEFAULT '[]',    -- JSON game.Laws, the laws in force
    status         TEXT NOT NULL DEFAULT 'alive', -- alive | fallen
    fallen_reason  TEXT,
    next_matter_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS senators (
    id          INTEGER PRIMARY KEY,
    guild_id    TEXT NOT NULL REFERENCES courts(guild_id),
    name        TEXT NOT NULL,
    traits      TEXT NOT NULL,  -- JSON list
    influence   REAL NOT NULL,
    base_price  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS senators_guild ON senators (guild_id);

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
    result      TEXT,  -- JSON summary once decided
    petitioner_id INTEGER  -- the character who petitioned for it, if anyone
);
CREATE INDEX IF NOT EXISTS matters_open ON matters (closes_at) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS matters_opened ON matters (guild_id, opened_at);
CREATE INDEX IF NOT EXISTS matters_resolved ON matters (guild_id, resolved_at);

CREATE TABLE IF NOT EXISTS votes (
    matter_id    INTEGER NOT NULL REFERENCES matters(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    option       TEXT NOT NULL,
    voted_at     INTEGER NOT NULL,
    locked       INTEGER NOT NULL DEFAULT 0, -- 1 once sold; can't be changed
    sold_to      TEXT,                       -- who bought it, for the record
    PRIMARY KEY (matter_id, character_id)
);
CREATE INDEX IF NOT EXISTS votes_character ON votes (character_id, voted_at);

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
    due_at    INTEGER NOT NULL,
    petitioner_id INTEGER -- set for a petition (see game.Petitions)
);
CREATE INDEX IF NOT EXISTS realm_queue_guild ON realm_queue (guild_id, due_at);

-- What each senator thinks of each player, -100 to 100. Missing = 0.
CREATE TABLE IF NOT EXISTS senator_opinions (
    senator_id   INTEGER NOT NULL REFERENCES senators(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    value        REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (senator_id, character_id)
);
CREATE INDEX IF NOT EXISTS senator_opinions_character ON senator_opinions (character_id);

-- How much of each sector a player owns. See game.Share.
CREATE TABLE IF NOT EXISTS sector_stakes (
    character_id INTEGER NOT NULL REFERENCES characters(id),
    guild_id     TEXT NOT NULL,
    sector       TEXT NOT NULL,
    stake        REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (character_id, sector)
);
CREATE INDEX IF NOT EXISTS sector_stakes_guild ON sector_stakes (guild_id, sector);

-- Temporary states on a character, like a wound. Expired rows are ignored.
CREATE TABLE IF NOT EXISTS conditions (
    character_id INTEGER NOT NULL REFERENCES characters(id),
    key          TEXT NOT NULL,
    ends_at      INTEGER NOT NULL,
    PRIMARY KEY (character_id, key)
);

-- Spectators' bets on a duel (an activity), paid when placed.
CREATE TABLE IF NOT EXISTS bets (
    activity_id  INTEGER NOT NULL REFERENCES activities(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    side_id      INTEGER NOT NULL REFERENCES characters(id), -- the duelist backed
    amount       INTEGER NOT NULL,
    placed_at    INTEGER NOT NULL,
    PRIMARY KEY (activity_id, character_id)
);

-- How deep owner's spies run in target's court, 0–100. Missing = 0.
CREATE TABLE IF NOT EXISTS spy_networks (
    owner_id  INTEGER NOT NULL REFERENCES characters(id),
    target_id INTEGER NOT NULL REFERENCES characters(id),
    value     REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (owner_id, target_id)
);
CREATE INDEX IF NOT EXISTS spy_networks_target ON spy_networks (target_id);

-- Public works a character has paid for, by work key (game.WorkDefs). One
-- of each per character. Their vote weight and value count forever.
CREATE TABLE IF NOT EXISTS monuments (
    character_id INTEGER NOT NULL REFERENCES characters(id),
    work_key     TEXT NOT NULL,
    built_at     INTEGER NOT NULL,
    PRIMARY KEY (character_id, work_key)
);

-- Crimes a player's spies reported under the Informers' Law, which the
-- owner may denounce once before they expire.
CREATE TABLE IF NOT EXISTS denunciations (
    id         INTEGER PRIMARY KEY,
    guild_id   TEXT NOT NULL,
    owner_id   INTEGER NOT NULL REFERENCES characters(id), -- who can denounce
    actor_id   INTEGER NOT NULL REFERENCES characters(id), -- who committed it
    crime      TEXT NOT NULL,   -- secret kind, e.g. 'bribe'
    detail     TEXT NOT NULL,   -- what the spies said
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,   -- unused: reports keep forever
    used_at    INTEGER
);
CREATE INDEX IF NOT EXISTS denunciations_owner ON denunciations (owner_id) WHERE used_at IS NULL;

-- Speak to a Senator: how often each player has called on each senator,
-- for their greeting.
CREATE TABLE IF NOT EXISTS senator_visits (
    senator_id   INTEGER NOT NULL REFERENCES senators(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    visits       INTEGER NOT NULL DEFAULT 0,
    last_at      INTEGER NOT NULL,
    PRIMARY KEY (senator_id, character_id)
);

-- What a senator told a player when asked how they'd vote on a matter
-- (game.ResolveAsk). Once per matter: asking again repeats the answer.
CREATE TABLE IF NOT EXISTS vote_asks (
    matter_id    INTEGER NOT NULL REFERENCES matters(id),
    senator_id   INTEGER NOT NULL REFERENCES senators(id),
    character_id INTEGER NOT NULL REFERENCES characters(id),
    reply        TEXT NOT NULL, -- game.AskReply
    option       TEXT NOT NULL, -- what they said ("" on a refusal)
    text         TEXT NOT NULL,
    asked_at     INTEGER NOT NULL,
    PRIMARY KEY (matter_id, senator_id, character_id)
);

-- ---- conspiracies ----

-- A plot's state. The plot itself is an activity of kind 'conspiracy'
-- (host = leader, goal = scheme, target_id = a character or a senator,
-- spend = the purse's gold, ends_at = when it falls apart).
CREATE TABLE IF NOT EXISTS conspiracies (
    activity_id  INTEGER PRIMARY KEY REFERENCES activities(id),
    target_kind  TEXT NOT NULL,              -- game.TargetKind
    target_item  INTEGER NOT NULL DEFAULT 0, -- estate_items.id to steal; 0 = their coffers
    purse        TEXT NOT NULL DEFAULT 'none',
    progress     REAL NOT NULL DEFAULT 0,
    warned       INTEGER NOT NULL DEFAULT 0, -- game.Discovery
    next_step_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS conspiracies_step ON conspiracies (next_step_at);

-- Who was asked into a plot, and what they said.
CREATE TABLE IF NOT EXISTS conspiracy_members (
    activity_id INTEGER NOT NULL REFERENCES activities(id),
    member_kind TEXT NOT NULL,    -- player | senator
    member_id   INTEGER NOT NULL, -- characters.id or senators.id
    status      TEXT NOT NULL,    -- invited | joined | declined | left
    invited_at  INTEGER NOT NULL,
    PRIMARY KEY (activity_id, member_kind, member_id)
);
CREATE INDEX IF NOT EXISTS conspiracy_members_member ON conspiracy_members (member_kind, member_id);

-- Things abroad in the realm for a few days: a manticore, a troll, a circus
-- (game.HappeningDefs). ended_at is set when it fades or someone ends it.
CREATE TABLE IF NOT EXISTS happenings (
    id         INTEGER PRIMARY KEY,
    guild_id   TEXT NOT NULL REFERENCES courts(guild_id),
    key        TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    ends_at    INTEGER NOT NULL,
    ended_at   INTEGER,
    ended_by   INTEGER REFERENCES characters(id) -- who ended it, if anyone
);
CREATE INDEX IF NOT EXISTS happenings_open ON happenings (ended_at, ends_at);
CREATE INDEX IF NOT EXISTS happenings_guild ON happenings (guild_id, ended_at);
