# Code Style and Architecture Guide for `whatsfordinner`

## General Principles
- Keep packages small and focused.
- Avoid god objects and massive files – split by domain (e.g. fridge, dinner, voting).
- All logic should be unit testable.
- Use dependency injection where practical.
- Think in terms of small services interacting via interfaces.
- Handle errors explicitly; never ignore them.

---

## Project Structure Example
```
/cmd/bot             # main telegram bot app
/pkg/
  fridge/            # fridge state management
  dinner/            # dinner planning logic
  poll/              # vote management
  cook/              # cooking tracking
  shopping/          # shopping list and buyers
  stats/             # leaderboard/statistics logic
  storage/           # embedded DB abstraction (Badger or Bolt)
  openai/            # LLM communication abstraction
  telegram/          # thin layer over tg APIs
/internal/
  test/              # shared mocks/test helpers
```

---

## Logging
- Use a structured logger (e.g., `logrus`, `zap`).
- Every major step should log: action, user, timestamp.
- Use contextual logs (e.g., per-channel ID).

---

## Tests
- Use `go test ./...` and CI to verify.
- Prefer `stretchr/testify` or `gotest.tools/assert` for assertions.
- For behavior-level testing, use `goconvey`, `godog`, or simple test harnesses with example dialogues.
- Mock all external services (Telegram, OpenAI).

---

## Telegram UX Guidelines
- Minimize need for typing.
- Use buttons (inline keyboards) for:
  - Voting yes/no
  - Volunteering to cook
  - Confirming shopping
  - Marking ingredients as used or missing
- Avoid sending big paragraphs.
- Emojis help: 🍽️ 🧑‍🍳 🛒 ✅ ❌ 📷 🍝 🥘

---

## Style Preferences
- Prefer Go-style naming (`CamelCase` for types, `lowerCamelCase` for vars).
- Avoid abbreviations unless widely known (e.g., API, DB).
- Group related files logically.
- Keep handlers short; push logic into use cases.

---

## Git & CI
- Feature branches: `feature/*`
- PRs must pass tests and linters.
- Use GitHub Actions to build and test.
- Tag main releases with semantic versioning.

---

## Dependencies
- Use only stable libraries.
- Pin all versions in `go.mod`
- Use `go mod tidy` and `go mod verify` in CI.

---

## Behavioral UX Patterns
- Each interaction should give clear feedback to the user.
- Polls and callback actions should always respond with a short confirmation message.
- Avoid hard failures in user flows. Gracefully degrade when OpenAI or Telegram fails.

---

## Linting & Formatting
- Use `go fmt`, `go vet`.
- Recommended: `golangci-lint`

---

## Language
- Use English everywhere (comments, logs, UI).
- Exceptions for cuisine names, user-added dish names, etc.

---

## Documentation
- Each package must have a `doc.go` with overview.
- Complex logic should be explained inline.
- Update README + TODO + examples as logic evolves.

