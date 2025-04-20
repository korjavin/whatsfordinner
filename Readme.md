# What's For Dinner Telegram Bot

A Telegram bot to help families (i.e., everyone in a Telegram group or channel) collaboratively decide what to cook for dinner. It facilitates suggestions, voting, cooking coordination, ingredient tracking, and dinner rating – all through interactive Telegram features and AI support.

---

## Features

- 📅 **Daily Dinner Planning** – Suggests 2–3 dinner options daily (around 3pm or via `/dinner` command).
- 🗳️ **Voting** – Starts Telegram poll to vote on the options.
- 👨‍🍳 **Cook Selection** – Asks if someone from the "pro" group is willing to cook. If not, restarts poll.
- 📷 **Fridge Inventory with Photo Recognition** – Add ingredients via chat or photo using OpenAI-compatible LLM.
- 🧾 **Shopping Helper** – Lists missing ingredients, lets someone volunteer to shop.
- 🍽️ **Dinner Completion** – Shares cooking instructions, tracks progress, and announces when dinner is ready.
- 🏆 **Family Stats** – Tracks and displays best cook, best helper, and best suggester based on past dinners.

---

## Workflow Summary

1. Around 15:00 or on `/dinner`, the bot checks fridge inventory.
2. Suggests 2–3 recipes based on available ingredients and cuisine preferences.
3. Starts a Telegram poll for family to vote.
4. Asks "pro" voters to volunteer to cook (via callback buttons).
5. If someone agrees, gives short recipe instructions with "more details" button.
6. Tracks cooking status.
7. Announces when dinner is ready.
8. After dinner, collects feedback and updates stats.
9. Updates fridge inventory with used ingredients.
10. Allows suggestions, ingredient sync, and reinitialization anytime.

---

## Commands

- `/dinner` – Starts or restarts the dinner suggestion flow.
- `/suggest` – Suggest your own dish before voting.
- `/fridge` – Show current ingredients.
- `/sync_fridge` – Trigger fridge re-initialization.
- `/add_photo` – Upload fridge photo for ingredient extraction.
- `/stats` – Show cooking/buying/suggestion leaderboards.

---

## Configuration

Via environment variables:

- `BOT_TOKEN`: Telegram Bot token
- `OPENAI_API_BASE`: Base URL for OpenAI-compatible LLM
- `OPENAI_API_KEY`: Auth token for LLM
- `OPENAI_MODEL`: LLM model name (e.g., gpt-4, gpt-3.5-turbo)
- `CUISINES`: Comma-separated list (default: European,Russian,Italian)

---

## Local Development

- Dev environment: Go + Podman + Podman Compose
- Storage: Embedded DB (e.g. BadgerDB or BoltDB)
- GitHub repo: https://github.com/korjavin/whatsfordinner
- Build: GitHub Actions with Docker build pipeline

### Running with Docker

You can run the bot using the pre-built Docker image from GitHub Container Registry:

```bash
# Pull the latest image
docker pull ghcr.io/korjavin/whatsfordinner:main

# Or with podman
podman pull ghcr.io/korjavin/whatsfordinner:main

# Run with environment variables
docker run -d \
  -v ./data:/app/data \
  -e BOT_TOKEN=your_telegram_bot_token \
  -e OPENAI_API_KEY=your_openai_api_key \
  -e OPENAI_API_BASE=https://api.openai.com/v1 \
  -e OPENAI_MODEL=gpt-3.5-turbo \
  -p 8083:8080 \
  ghcr.io/korjavin/whatsfordinner:main
```

### CI/CD Pipeline

The project uses GitHub Actions to automatically build and push Docker images to GitHub Container Registry (GHCR):

- Images are built on every push to the `main` branch
- Tagged releases (e.g., `v1.0.0`) create versioned Docker images
- Pull requests build images but don't push them to the registry

---

## UX Guidelines

- Designed for mobile: short texts, inline buttons, no complex commands.
- All users in the Telegram channel = family.
- Each channel handled as an isolated family (independent state).

---

## Notes

- If nobody votes, bot sends warning after 60 minutes and closes with "no-dinner-today".
- Ingredient inventory can become stale – allow manual updates and sync.
- All flows are logged with timestamps for debugging.

---

## Example Dialogues

See files in `examples/dialogues/` for sample flows.

---

## License

MIT

