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
                 senate (senators, traits, voting, PickMatter cooldown/themes),
                 personas.go (the famous figures senators are drawn from, with fixed traits),
                 matters.go + matters_more.go + matters_magic.go (Senate event catalogue), arcs.go + matters_arcs.go (story arcs, Chronicle), events_magic.go (mystical party/hunt events), catalogue.go (shop items, daily market),
                 opinion.go (senator opinion of players, Court a Senator),
                 sectors.go (the realm's industries, Invest in a Venture),
                 duel.go, hunt.go (Martial actions), conditions.go (wounds), timings.go,
                 influence.go (ScaleInfluence, FreeKinds, Canvass, Bread and Circuses), speech.go,
                 works.go (public works and monuments),
                 spies.go (spy networks, Plant/Root Out Spies, Practice), secrets.go (what spies leak)
internal/store   SQLite: schema.sql (applied on open, no migrations), characters,
                 activities (parties, courting), senate (realm, matters, votes, bribes, vote deals),
                 opinions, ventures (sector stakes, investments), duels (accept/decline, bets),
                 conditions, spies (spy networks), estate (buying, selling, moving, estate offers),
                 monuments (in activities.go)
internal/bot     Discord: commands.go (registration + dispatch), creator, menu, party, court,
                 invest, duel, hunt, intrigue (spies), leak, senate, votetrade, estate, shop,
                 estatetrade (player deals), influence (canvass, circus, announce helpers),
                 speech, works, info pages
```
Custom IDs are `prefix:part:part` and dispatched on the prefix in `commands.go` (`cc`, `menu`, `party`, `court`, `invest`, `duel`, `hunt`, `spy`, `canvass`, `speech`, `circus`, `work`, `estate`, `shop`, `deal`, `senate`, `info`). Each file documents its IDs at the top.

## Conventions
- Content and numbers are Go data in `internal/game`; keep it data-driven.
- Rolls are hidden from players; never show chances in Discord.
- Stats are stored as JSON, so new stats need no migration. Estate value and item effects are derived from `estate_items`, so use `Character.Effective()`, `Sheet()`, `VoteWeight()`, `TotalIncome()` rather than raw fields.
- Activities (`activities` table) are timed actions, one hosted per character at a time; the tick in `bot.go` resolves them by `Kind`. A new action needs a resolver there and a panel in `menu.go`'s `act` dispatch. Resolve through `store.ApplyResult(…, store.Outcome{Deltas, Opinions, Conditions, Spies, Realm, Monuments}, …)`; `Realm` changes the court's trackers (clamped). Kinds in `game.FreeKinds` (public works) don't take the slot: `HostedActivity` ignores them, `WorkInProgress` finds them. `b.announce`/`b.announceEnd` post and close a public activity's announcement.
- Everyday influence gains (canvassing, speeches, circuses) go through `game.ScaleInfluence(gain, current)` so the most powerful player can't grind the Senate. Monuments (`monuments` table, `Character.Monuments`) add permanent vote weight and estate value via `Effects()`/`EstateValue()`; they are not estate items, so the shop and player deals never touch them. DM events (`party:event:…`) work for any activity: put the catalogue in `game.EventByKey`, and `NewEvent.CharacterID` picks who gets it (0 = host). A duel keeps its state (`pending`/`accepted`) in the activity's `goal`.
- Conditions (`game.ConditionDefs`, e.g. Wounded) are temporary effects stored in `conditions` and loaded with every character; `Character.Effects()` includes them. Give one via `game.ConditionChange` → `Bot.newConditions` (durations are in game days, `Timings.Day`).
- Spy levels (0–100, `spy_networks`) are hidden; players only see the tier word (`game.SpyTierOf`). **Every private action calls `b.leak(ctx, actor, game.Secret…)`** after it succeeds, so spies can report it; add a constructor in `secrets.go` for new ones. Change networks via `Outcome.Spies` or `ChangeSpies`.
- Actions give a little practice in the stat they use: merge `game.Practice(stat, success)` into the actor's delta.
- Story arcs (`arcs.go`, `matters_arcs.go`): a matter's `Arc`/`Chapter` place it in a story; only chapter 1 opens on its own, later chapters are `FollowUpOnly`. In **strict** arcs every outcome of every chapter before the last must queue a later chapter with chance 1 (`TestArcsAreWellFormed` enforces it). The bot picks with `PickMatterAvoiding` (never reopens a running arc, from `store.QueuedEvents`), and `/realm` shows `game.Chronicle`. Sectors: Alchemy replaced Manufacturing; the secret sector is the Black Market (`SectorBlackMarket`).
- Losing the realm: senators refuse options whose worst case ends the realm (`MatterOption.RealmDanger`; Reckless senators use the expected case). Danger comes from what they can't see: **Auto** events (`MatterEvent.Auto`, one option, no vote, no drift) are bills queued as follow-ups by cheap-now options. Anything that changes trackers outside a matter goes through `Outcome.Realm`/`applyRealm` (which marks the realm fallen at 0); snapshot with `b.realmBefore` and call `b.afterRealmChange` to post `game.RealmWarnings` and announce a fall.
- All real-time durations live in `game.Timings` (`DefaultTimings`/`FastTimings`).
- Senator opinion (-100..100) is shown to players; its effects are described in words, never with the numbers behind them. Change it through `game.OpinionChange` (SenatorID 0 = every senator); use `BribePriceFor`/`VoteOfferFor`, not the raw versions, anywhere a player is involved.
- Sectors: the realm's state is `realms.sectors` (JSON `game.SectorState`), stakes are `sector_stakes`. `TotalIncome()` includes venture income, which the store fills in on every character load (`fillVentures`). Senate outcomes change sectors through `MatterOutcome.Sectors` (Health is a temporary shock, Capacity is permanent). Sector revenue and shares are shown to players; the investment deal roll is not.
- Estate: `Character.Items`/`Holdings` are loaded **residence first** (`characters.residence_id`), and `game.EstateValue`/`SumEffects` rely on that: the residence and possessions count fully, other properties half and only for income and stats. Checks see `game.EstateScore(value)` (diminishing), via `NewSheet`. Unique items have one owner per court (`estate_items.guild_id`); `game.Market(guild, period)` is a pure seeded draw, so stock never reshuffles mid-day. Every character keeps at least one property: `SellToShop`/`AcceptEstateOffer` enforce it.
- Senate picks: no event repeats within `game.MatterCooldown` matters (read from the `matters` table), and two random picks in a row never share a `Theme`. Every new event needs a Theme.
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
