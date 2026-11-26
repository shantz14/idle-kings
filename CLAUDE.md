# Idle Kings

Discord bot idle game of court politics. Go + SQLite (pure-Go `modernc.org/sqlite`, no cgo), `bwmarrin/discordgo` v0.29.

## Design lives in the vault
`~/Dev/vault67/claude/Idle Kings.md` is the design doc. Read it before changing game rules.
- Isaac's sections (top) hold **his** design, game design only, no implementation.
- Anything Claude invents (events, numbers, backgrounds, items) goes under **Claude's suggestions** at the bottom, clearly marked.
- When Isaac describes new design, write it into the note **and** build it.
- Vault notes follow the template in `~/.claude/CLAUDE.md`.

## Layout
```
cmd/idle-kings   entrypoint, env config
internal/game    pure rules, no Discord/DB: stats, Check (sheet → % chance), creation
                 (creation, crest, backgrounds, roles, estate items), party, realm,
                 senate (senators, traits, voting), matters.go (Senate event catalogue)
internal/store   SQLite: schema.sql (applied on open, no migrations), characters,
                 activities (parties), senate (realm, matters, votes, bribes, vote deals)
internal/bot     Discord: commands.go (registration + dispatch), creator, menu, party,
                 senate, votetrade, info pages
```
Custom IDs are `prefix:part:part` and dispatched on the prefix in `commands.go` (`cc`, `menu`, `party`, `senate`, `info`). Each file documents its IDs at the top.

## Conventions
- Content and numbers are Go data in `internal/game`; keep it data-driven.
- Rolls are hidden from players; never show chances in Discord.
- Stats are stored as JSON, so new stats need no migration. Estate value and item effects are derived from `estate_items`, so use `Character.Effective()`, `Sheet()`, `VoteWeight()`, `TotalIncome()` rather than raw fields.
- The store uses a single DB connection: close `rows` before running another query (see `listCharacters`), and never call non-tx store methods inside a transaction.
- Schema is dev-only. Changing it means deleting `idle-kings.db`; say so in the README.

## Checks
```sh
go vet ./... && go test ./...
IDLE_KINGS_SIM=1 go test ./internal/game -run TestBalance -v   # after changing Senate events/numbers
```
`internal/bot/limits_test.go` checks every popup against Discord's component and embed limits; add new views to it.
The bot can't be exercised without Isaac's token. Run it with `IDLE_KINGS_FAST=1 DISCORD_TOKEN=... go run ./cmd/idle-kings` (short timers).

## Open questions
- Restarting after the realm falls isn't designed.
- Wars are next: voting on which senator to send as general. Hook them into Senate follow-ups.
- Estate shop (buying items) isn't built; the item catalogue is ready for it.
- Role-specific actions/events: hooks exist (`ActionDef.Roles`, `PartyEvent.Roles`, `MatterOutcome.Roles`), no content yet.
