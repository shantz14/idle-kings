# idle-kings

Idle court-intrigue game played through a Discord bot. Go + SQLite (pure-Go `modernc.org/sqlite`, no cgo).

Design notes live in the vault: `claude/Idle Kings.md`.

## Run

```sh
DISCORD_TOKEN=... go run ./cmd/idle-kings
```

| Env | Meaning |
| --- | --- |
| `DISCORD_TOKEN` | Bot token (required) |
| `IDLE_KINGS_DB` | SQLite path, default `idle-kings.db` |
| `IDLE_KINGS_FAST` | `1` for testing: parties last 4 minutes instead of 2 hours, and a Senate "day" is 6 minutes (4 to vote) |

Invite the bot with the `bot` and `applications.commands` scopes. Commands are registered globally so they also work in DMs with the bot.

The schema isn't migrated yet: after pulling schema changes, delete `idle-kings.db`.

## Game flow (current)

1. `/claim-admin` — first person to run it becomes the server's admin.
2. `/start announcements:#channel` — admin only. Posts a **Join the court** button in this channel; public events go to the announcements channel (defaults to this one).
3. **Join the court** (or `/join`) opens the character creator, a private popup: name, family name and motto → crest (color + symbol emoji) → assign 6 stat points → background (8 backstories; sets renown, gold and the starting estate) → role. Progress is saved, so a closed popup resumes where it left off. Family names are unique per court.
4. `/court` (in the server or in a DM with the bot) opens a button menu: Diplomacy, Monetary, Intrigue, Martial, The Senate, My Character, Estate.
5. Diplomacy → **Throw a Party** → pick a goal. The party is announced with an **Attend** button; the host gets 1–2 events by DM with buttons. Everything resolves when the party ends; results arrive by DM, and a summary is posted publicly.
6. **The Senate** (`/senate`, or The Senate in `/court`): every day a matter is posted in the announcements channel with vote buttons. Votes are weighted by influence against 8 AI senators with traits. Players can bribe senators, sell their vote to a senator or a player, or offer to buy another player's vote (deals are accepted in DMs). Results, everyone's votes and the realm's trackers are posted when voting closes. If a realm tracker hits 0 the realm falls and everyone loses.
7. Daily income (base income plus estate items) is paid every real day.
8. Info pages: `/realm`, `/players`, `/profile [player]` (add `public: True` to post them for everyone), `/character`, `/estate`, `/help`. Other players see your house, role, background, influence, renown and estate; only you see your stats and gold.

## Layout

```
cmd/idle-kings     entrypoint, env config
internal/game      pure rules: stats, Check (sheet → % chance), character creation (creation.go,
                   crest.go, backgrounds.go, roles.go, estate.go items), party rules and events,
                   realm trackers, senators and traits, daily matters (matters.go), vote resolution
internal/store     SQLite schema + queries (characters, activities, realm, senators, matters, votes, bribes, vote deals)
internal/bot       slash commands, button menus, party flow, game loop
```

To check balance after changing events or numbers: `IDLE_KINGS_SIM=1 go test ./internal/game -run TestBalance -v` simulates 300 AI-only realms for 90 days.

Content and numbers (roles, events, weights) are Go data in `internal/game`. Character stats are stored as JSON so adding a stat needs no migration. Rolls are never shown to players.
