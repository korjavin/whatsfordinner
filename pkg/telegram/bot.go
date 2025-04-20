package telegram

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/korjavin/whatsfordinner/pkg/logger"
)

// Bot represents a Telegram bot instance
type Bot struct {
	api    *tgbotapi.BotAPI
	logger *logger.Logger
}

// HandlerFunc is a function that handles a Telegram update
type HandlerFunc func(update tgbotapi.Update)

// CommandHandler is a function that handles a Telegram command
type CommandHandler func(message *tgbotapi.Message)

// CallbackHandler is a function that handles a Telegram callback query
type CallbackHandler func(callback *tgbotapi.CallbackQuery)

// New creates a new Telegram bot instance
func New(token string) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Telegram bot: %w", err)
	}

	bot := &Bot{
		api:    api,
		logger: logger.New(""),
	}

	bot.logger.Info("Telegram bot created: @%s", api.Self.UserName)
	return bot, nil
}

// Start starts the bot and listens for updates
func (b *Bot) Start(commandHandlers map[string]CommandHandler, callbackHandlers map[string]CallbackHandler, defaultHandler HandlerFunc) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		// Create a channel-specific logger if we have a chat ID
		var chatID int64
		if update.Message != nil {
			chatID = update.Message.Chat.ID
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
		}

		if chatID != 0 {
			b.logger = logger.New(fmt.Sprintf("%d", chatID))
		}

		// Handle commands
		if update.Message != nil && update.Message.IsCommand() {
			command := update.Message.Command()
			if handler, ok := commandHandlers[command]; ok {
				b.logger.Info("Handling command: %s from user %s", command, update.Message.From.UserName)
				handler(update.Message)
				continue
			}
		}

		// Handle callback queries
		if update.CallbackQuery != nil {
			data := update.CallbackQuery.Data
			for prefix, handler := range callbackHandlers {
				if len(data) >= len(prefix) && data[:len(prefix)] == prefix {
					b.logger.Info("Handling callback: %s from user %s", data, update.CallbackQuery.From.UserName)
					handler(update.CallbackQuery)
					break
				}
			}
			continue
		}

		// Use default handler for other updates
		if defaultHandler != nil {
			defaultHandler(update)
		}
	}

	return nil
}

// SendMessage sends a text message to a chat
func (b *Bot) SendMessage(chatID int64, text string) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	return b.api.Send(msg)
}

// SendMessageWithKeyboard sends a text message with an inline keyboard
func (b *Bot) SendMessageWithKeyboard(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	return b.api.Send(msg)
}

// CreatePoll creates a poll in a chat
func (b *Bot) CreatePoll(chatID int64, question string, options []string) (tgbotapi.Message, error) {
	poll := tgbotapi.NewPoll(chatID, question, options...)
	poll.IsAnonymous = false
	return b.api.Send(poll)
}

// AnswerCallbackQuery answers a callback query
func (b *Bot) AnswerCallbackQuery(callbackID string, text string) error {
	callback := tgbotapi.NewCallback(callbackID, text)
	_, err := b.api.Request(callback)
	return err
}

// EditMessage edits a message
func (b *Bot) EditMessage(chatID int64, messageID int, text string) (tgbotapi.Message, error) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	return b.api.Send(edit)
}

// EditMessageKeyboard edits a message's inline keyboard
func (b *Bot) EditMessageKeyboard(chatID int64, messageID int, keyboard tgbotapi.InlineKeyboardMarkup) (tgbotapi.Message, error) {
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID, keyboard)
	return b.api.Send(edit)
}

// Send sends a Chattable to Telegram
func (b *Bot) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	return b.api.Send(c)
}
