# TODO List for `whatsfordinner` Telegram Bot

## 1. Project Setup
- [x] Initialize Go module
- [x] Setup `.env` configuration parsing (Telegram token, OpenAI config, etc.)
- [x] Embed BadgerDB as local storage
- [x] Telegram bot client (polling)
- [x] Logging setup (structured + timestamps)

## 2. Data Models
- [x] ChannelState: track per-channel data
- [x] Fridge: current ingredients list
- [x] Dish: name, ingredients, instructions, cuisine type
- [x] VoteState: current voting process, participants
- [x] CookStatus: cooking progress
- [x] Statistics: cook, helper, suggester leaderboards

## 3. Bot Command Handlers
- [x] `/dinner` – Suggest dinner, start poll, manage vote lifecycle
- [x] `/suggest` – Allow manual dish suggestion
- [x] `/fridge` – Show current ingredients
- [x] `/sync_fridge` – Reinitialize fridge
- [x] `/add_photo` – Use photo to extract ingredients
- [x] `/add` – Use text to extract ingredients and add them to fridge
- [x] `/stats` – Show family leaderboards

## 4. Voting and Cooking Flow
- [x] Suggest 2–3 dishes with matching fridge contents and cuisine filter
- [x] Create Telegram poll, wait for majority vote
- [x] Ask willing cook from the "pro" voters
- [ ] If none agree in 10 minutes, retry cooking step
- [ ] If still nobody agrees, cancel vote and mark "no dinner today"
- [ ] Pick random cook from volunteers and share instructions
- [x] Provide callbacks for more details, progress updates
- [x] Confirm when dinner is ready

## 5. Fridge Inventory
- [x] Initial entry via chat
- [x] AI image extraction (OpenAI Vision API)
- [ ] Ingredient used marking via cook UI
- [ ] Sync fridge items manually with buttons ("We don’t have this anymore")

## 6. Shopping Flow
- [ ] Check for missing ingredients
- [ ] List what's needed, offer "I will buy" button
- [ ] Broadcast confirmation to family
- [ ] Confirm when shopping is done

## 7. Feedback Collection
- [x] Post-dinner rating collection
- [x] Update statistics: best cook, helper, suggester

## 8. Persistent State Management
- [x] Use per-channel keying
- [x] Safe concurrent access

## 9. GitHub Actions & Containerization
- [x] Setup Dockerfile
- [ ] Setup GitHub Actions CI/CD
- [x] Podman Compose config for local running


## 12. Caching
- [ ] Cache dish suggestions
- [ ] Cache ingredient extraction results
- [ ] Cache OpenAI responses if the question is exactly the same

## 13. Final Touches
- [ ] Automatic cleanup of old polls/dinners
- [ ] Backup/export fridge and stats

