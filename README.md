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
| `IDLE_KINGS_FAST` | `1` for testing: parties last 4 minutes instead of 2 hours, investments 3 minutes instead of 3 hours, and a Senate "day" is 6 minutes (4 to vote) |

Invite the bot with the `bot` and `applications.commands` scopes. Commands are registered globally so they also work in DMs with the bot.

The schema isn't migrated yet: after pulling schema changes, delete `idle-kings.db`. (The senator opinion system added the `senator_opinions` table and `activities.target_id`, party budgets added `activities.spend`, ventures added `realms.sectors` and the `sector_stakes` table, duels and hunts added the `conditions` and `bets` tables, spies added the `spy_networks` table, public works added the `monuments` table, the Alchemy and Black Market sectors renamed sector keys in `realms.sectors`, the estate shop added `characters.residence_id`, `estate_items.guild_id`/`price_paid` and the `estate_offers` table (and dropped `realms.recent`), and laws added `realms.laws`, `matters.petitioner_id`, `realm_queue.petitioner_id` and the `denunciations` table, so older databases need deleting. Stats moved to a ×5 scale, so characters made before it need deleting too. Speak to a Senator only added the `senator_visits` and `vote_asks` tables, which older databases pick up on open.)

## Game flow (current)

1. `/claim-admin` — first person to run it becomes the server's admin.
2. `/start announcements:#channel senate:standard|random` — admin only. Posts a **Join the court** button in this channel; public events go to the announcements channel (defaults to this one). `senate` picks the balanced **standard Senate** (the default: the same 8 senators every game) or **8 drawn at random** from every famous figure.
3. **Join the court** (or `/join`) opens the character creator, a private popup: name, family name and motto → crest (color + symbol emoji) → assign 6 stat points → background (8 backstories; sets renown, gold and the starting estate) → role. Progress is saved, so a closed popup resumes where it left off. Family names are unique per court.
4. `/court` (in the server or in a DM with the bot) opens a button menu: Diplomacy, Monetary, Intrigue, Martial, The Senate, My Character, Estate.
5. Diplomacy → **Speak to a Senator** → pick a senator. They greet you; you can **ask how they'll vote** (once per matter; they may refuse or lie), bribe them, sell them your vote, or **court** them: pick an approach (flatter, gift, share secrets, talk strategy). It takes an hour; the result arrives by DM and changes that senator's opinion of you. A player can run one action (party, courting or investing) at a time.
6. Diplomacy → **Throw a Party** → pick a goal. The party is announced with an **Attend** button; the host gets 1–2 events by DM with buttons. Everything resolves when the party ends; results arrive by DM, and a summary is posted publicly.
7. **The Senate** (`/senate`, or The Senate in `/court`): every day a matter is posted in the announcements channel with vote buttons. Votes are weighted by influence against 8 AI senators with traits. Players can bribe senators, sell their vote to a senator or a player, or offer to buy another player's vote (deals are accepted in DMs). Results, everyone's votes and the realm's trackers are posted when voting closes. Diplomacy → **Petition the Senate** puts a law (or its repeal) to a vote straight away, alongside the day's matter. If a realm tracker hits 0 the realm falls and everyone loses.
8. **Opinion**: each senator has an opinion of each player (-100 to +100, shown with 🟢/🔴). Votes, bribes, vote sales, parties, rumors and courting move it; it fades 3% per Senate day. It scales bribe prices and senators' offers for your vote, makes hostile senators refuse to deal, and pulls senators toward (or away from) your side on close votes.
9. Monetary → **Invest in a Venture** → pick one of 7 industries (Agriculture, Mining, Alchemy & Apothecaries, Luxury Goods, Trade, Timber & Shipbuilding, the Black Market) and an amount. After 3 hours a hidden stewardship roll (intrigue for the Black Market) decides how big a share the gold buys. Each sector's daily revenue is split between its owners; NPC houses hold the rest. Senate decisions shock sectors (barbarians burn the farms, a trade pact fills the harbor), and realm trackers shift them too, so votes move players' income. Stakes in the Black Market are secret, unless a deal goes badly wrong. Pie charts show who owns each industry: the chosen one on the Invest screen, all of them on `/realm`, and yours on `/character` (in the Black Market you only see yourself).
10. Martial → **Challenge to a Duel** → pick a player and an optional gold wager. The defender accepts or declines by DM (declining or not answering in 4 hours costs a little renown). Once accepted the duel is announced with **Bet** buttons and fought 30 minutes later; each duelist picks a stance by DM (attack > trick > guard > attack, or yield). The winner takes renown and the purse, bettors on the winner split the pot, and a bad loss leaves the loser **Wounded** (−10 Prowess, −5 Martial for 3 days).
11. Martial → **Hunt in the Royal Forest** → pick a goal (Chase Glory or Talk in the Saddle). Costs 10 gold, takes 2 hours, runs like a party: host events by DM, and others can **Join the hunt** to hunt for glory, ride with friends, or arrange an accident for another rider.
12. Martial → **Undertake a Journey** → pick a destination: escort a trade caravan or tour the provinces (3 hours), or go abroad to Carthage, a tourney in Gondor or the Library of Alexandria (6 hours, sometimes a foreign gift). Road events come by DM. The departure is announced, and anyone can secretly **Hire bandits** from the post (three bands, dearer is tougher). The bandits rob the traveller, partly rob them, are beaten off (prowess, martial and renown for the traveller), or are beaten and one names who paid them. The hirer gets half the loot.
13. A player can host one action at a time (party, courting, investing, hunt, speech, canvassing, bread and circuses, spying, or an issued challenge); a public work being built doesn't count. Answering a challenge, attending, riding along and betting don't count.
14. **Influence actions.** Diplomacy → **Canvass the Districts** (3 hours, free, always a little influence) and **Give a Speech** (pick a cause; public; others attend to cheer or heckle, a Diplomacy contest; senators who share the cause warm to a good speaker). Monetary → **Bread and Circuses** (30–1000 gold; influence with diminishing returns, raises realm Popularity, Populists approve) and **Endow a Public Work** (statue/road/temple/aqueduct, 150–800 gold, 1–4 days to build, doesn't use your action; when it opens you get permanent Senate vote weight and a named monument on your profile). Influence from canvassing, speeches and circuses shrinks as your influence grows (half at 30).
15. **Story arcs**: some Senate matters are chapters in a story (🌲 Baba Yaga of the Ashwood, ☄️ The Fallen Star, 💀 Acererak, the Barrow King, ⚗️ The Elixir of Youth, plus the older chains: the Dothraki, Robin Hood, the Plague, the Ironborn…). Each chapter's decision leads to the next, branching on what the Senate chose; the announcement shows "Chapter N" (never how many are left), and `/realm` has a **Chronicle** of where every story stands. An arc can't start again while it's running. Mystical events (omens, ghosts, the fey, sorcerers, alchemists) appear in the Senate, at parties and on hunts.
16. **Estate and shop** (`/shop`, or Shop on `/estate`): three tabs: **Properties** (humble rooms up to castles), **Estate Goods** (art, artifacts, war relics, keepsakes) and the **Armory** (weapons, armor and mounts, which raise prowess). A fixed shelf of common goods is always in stock; a rotating market of 2 unique pieces per tab changes daily (the Fine house and the Castle only appear there). Shop prices are 20% under list (`ShopPriceScale`) (every 10 minutes in fast mode). Unique pieces have one owner per court. You live in one property (move on `/estate`); it and your possessions count fully toward estate value, other properties half and only for income and stats. Sell back to the shop for 60%, or make deals with other players (sell, buy or trade, with gold either way) by DM; you always keep at least one property. Weapons and armor raise prowess for duels and hunts.
17. **Senate variety**: an event can't come back within 10 matters, and two in a row never share a theme (barbarians, bandits, food, sea…). Follow-ups can break the cooldown but never repeat the matter just before.
18. Daily income (base income, estate items and ventures) is paid every real day.
19. Info pages: `/realm` (now with Industries), `/players`, `/profile [player]` (add `public: True` to post them for everyone), `/character`, `/estate`, `/shop`, `/help`. Other players see your house, role, background, influence, renown and estate; only you see your stats and gold.
20. **Dossier** (Intrigue → Your Dossier): when your spies catch a rival doing something shameful (embezzling, bribery, selling or trading votes, hiring bandits, spreading rumors, planting spies, enchanted goods under the Witchcraft Act) in enough detail, the report is filed and keeps forever. Denounce it straight from the DM or any time later: it shames them before the court; under the Informers' Law it hits harder and the treasury pays you. Or **Whisper it**: softer and nothing is posted; they only get "someone is spreading the rumor that you…", unless their spies on you catch who.

## Layout

```
cmd/idle-kings     entrypoint, env config
internal/game      pure rules: stats, Check (sheet → % chance), character creation (creation.go,
                   crest.go, backgrounds.go, roles.go, estate.go items), party rules and events,
                   realm trackers, senators and traits, daily matters (matters.go), vote resolution,
                   senator opinion and Court a Senator (opinion.go), sectors and investing (sectors.go),
                   duels (duel.go), hunts (hunt.go), conditions like wounds (conditions.go), timings.go
internal/store     SQLite schema + queries (characters, activities, realm, senators, matters, votes, bribes,
                   vote deals, senator opinions, sector stakes, duels and bets, conditions)
internal/bot       slash commands, button menus, party, courting, investing, duel and hunt flows, game loop
```

To check balance after changing events or numbers: `IDLE_KINGS_SIM=1 go test ./internal/game -run TestBalance -v` simulates 300 AI-only realms for 90 days.

Content and numbers (roles, events, weights) are Go data in `internal/game`. Character stats are stored as JSON so adding a stat needs no migration. Rolls are never shown to players.
