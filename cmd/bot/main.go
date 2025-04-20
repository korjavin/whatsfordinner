package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/korjavin/whatsfordinner/pkg/config"
	"github.com/korjavin/whatsfordinner/pkg/dinner"
	"github.com/korjavin/whatsfordinner/pkg/fridge"
	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/messages"
	"github.com/korjavin/whatsfordinner/pkg/openai"
	"github.com/korjavin/whatsfordinner/pkg/poll"
	"github.com/korjavin/whatsfordinner/pkg/state"
	"github.com/korjavin/whatsfordinner/pkg/storage"
	"github.com/korjavin/whatsfordinner/pkg/telegram"
)

func main() {
	// Initialize logger
	log := logger.Global
	log.Info("Starting WhatsForDinner bot...")

	// Load configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Error("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// Initialize storage
	dataDir := filepath.Join(".", "data")
	store, err := storage.New(dataDir)
	if err != nil {
		log.Error("Failed to initialize storage: %v", err)
		os.Exit(1)
	}
	defer store.Close()

	// Start BadgerDB garbage collection
	store.StartGCRoutine(10 * time.Minute)

	// Initialize OpenAI client
	openaiClient := openai.New(cfg.OpenAIAPIKey, cfg.OpenAIAPIBase, cfg.OpenAIModel)

	// Initialize services
	fridgeService := fridge.New(store)
	dinnerService := dinner.New(store, fridgeService, openaiClient)
	pollService := poll.New(store)
	messageService := messages.New(openaiClient)
	stateManager := state.New()

	// Initialize Telegram bot
	bot, err := telegram.New(cfg.BotToken)
	if err != nil {
		log.Error("Failed to initialize Telegram bot: %v", err)
		os.Exit(1)
	}

	// Setup command handlers
	commandHandlers := map[string]telegram.CommandHandler{
		"start": func(message *tgbotapi.Message) {
			welcomeMsg := messageService.GenerateWelcomeMessage()
			bot.SendMessage(message.Chat.ID, welcomeMsg)
		},
		"dinner": func(message *tgbotapi.Message) {
			// Start dinner suggestion flow
			chatID := message.Chat.ID
			
			// Suggest dishes based on available ingredients and cuisine preferences
			dishes, err := dinnerService.SuggestDishes(chatID, cfg.Cuisines, 3)
			if err != nil {
				log.Error("Failed to suggest dishes: %v", err)
				errorMsg := messageService.GenerateErrorMessage("suggest dishes")
				bot.SendMessage(chatID, errorMsg)
				return
			}
			
			if len(dishes) == 0 {
				bot.SendMessage(chatID, "😢 I couldn't find any suitable dishes based on your fridge contents. Try adding more ingredients with /fridge.")
				return
			}
			
			// Create options for the poll
			options := make([]string, len(dishes))
			dishNames := make([]string, len(dishes))
			
			for i, dish := range dishes {
				options[i] = dish.Name
				dishNames[i] = fmt.Sprintf("%s (%s)", dish.Name, dish.Cuisine)
			}
			
			// Generate message with dish suggestions
			msgText := messageService.GenerateDinnerSuggestions(dishNames)
			
			// Create poll
			pollMsg, err := bot.CreatePoll(chatID, "What should we cook tonight?", options)
			if err != nil {
				log.Error("Failed to create poll: %v", err)
				errorMsg := messageService.GenerateErrorMessage("create poll")
				bot.SendMessage(chatID, errorMsg)
				return
			}
			
			// Store vote state
			_, err = pollService.CreateVote(chatID, fmt.Sprintf("%d", pollMsg.MessageID), pollMsg.MessageID, options)
			if err != nil {
				log.Error("Failed to create vote state: %v", err)
			}
			
			// Send the suggestions message
			bot.SendMessage(chatID, msgText)
		},
		"fridge": func(message *tgbotapi.Message) {
			// Show current ingredients
			chatID := message.Chat.ID
			
			ingredients, err := fridgeService.ListIngredients(chatID)
			if err != nil {
				log.Error("Failed to list ingredients: %v", err)
				errorMsg := messageService.GenerateErrorMessage("retrieve fridge contents")
				bot.SendMessage(chatID, errorMsg)
				return
			}
			
			if len(ingredients) == 0 {
				emptyFridgeMsg := messageService.GenerateEmptyFridgeMessage()
				bot.SendMessage(chatID, emptyFridgeMsg)
				return
			}
			
			// Extract ingredient names
			ingredientNames := make([]string, len(ingredients))
			for i, ingredient := range ingredients {
				if ingredient.Quantity != "" {
					ingredientNames[i] = fmt.Sprintf("%s (%s)", ingredient.Name, ingredient.Quantity)
				} else {
					ingredientNames[i] = ingredient.Name
				}
			}
			
			// Generate message with ingredients
			msgText := messageService.GenerateFridgeContentsMessage(ingredientNames)
			
			bot.SendMessage(chatID, msgText)
		},
		"sync_fridge": func(message *tgbotapi.Message) {
			// Reset the fridge
			chatID := message.Chat.ID
			
			err := fridgeService.ResetFridge(chatID)
			if err != nil {
				log.Error("Failed to reset fridge: %v", err)
				errorMsg := messageService.GenerateErrorMessage("reset fridge")
				bot.SendMessage(chatID, errorMsg)
				return
			}
			
			// Set the chat state to adding ingredients
			stateManager.SetState(chatID, state.StateAddingIngredients)
			
			bot.SendMessage(chatID, "🧹 Fridge reset! Now, please send me a list of ingredients you have. You can send multiple messages, and I'll add all the ingredients to your fridge.")
		},
		// TODO: Implement other command handlers
	}

	// Setup callback handlers
	callbackHandlers := map[string]telegram.CallbackHandler{
		// TODO: Implement callback handlers
	}

	// Setup default handler
	defaultHandler := func(update tgbotapi.Update) {
		// Handle text messages
		if update.Message != nil && update.Message.Text != "" && !update.Message.IsCommand() {
			chatID := update.Message.Chat.ID
			text := update.Message.Text
			
			// Check if the chat is in adding ingredients state
			if stateManager.GetState(chatID) == state.StateAddingIngredients {
				// Parse ingredients from the text
				ingredients, err := openaiClient.ParseIngredientsFromText(text)
				if err != nil {
					log.Error("Failed to parse ingredients: %v", err)
					bot.SendMessage(chatID, fmt.Sprintf("😢 Sorry, I couldn't understand the ingredients. Please try again with a clearer list."))
					return
				}
				
				if len(ingredients) == 0 {
					bot.SendMessage(chatID, "I couldn't find any ingredients in your message. Please try again with a list of ingredients.")
					return
				}
				
				// Add ingredients to the fridge
				for _, ingredient := range ingredients {
					err := fridgeService.AddIngredient(chatID, ingredient, "")
					if err != nil {
						log.Error("Failed to add ingredient %s: %v", ingredient, err)
					}
				}
				
				// Confirm the ingredients were added
				bot.SendMessage(chatID, fmt.Sprintf("✅ Added %d ingredients to your fridge: %s", len(ingredients), strings.Join(ingredients, ", ")))
				
				// Ask if they want to add more
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Done adding ingredients", "done_adding"),
						tgbotapi.NewInlineKeyboardButtonData("Add more", "add_more"),
					),
				)
				
				msg := tgbotapi.NewMessage(chatID, "Would you like to add more ingredients or are you done?")
				msg.ReplyMarkup = keyboard
				bot.Send(msg)
			} else {
				// Regular ingredient adding (single ingredient)
				// Check if it looks like an ingredient
				if !strings.Contains(text, " ") && len(text) < 30 {
					err := fridgeService.AddIngredient(chatID, text, "")
					if err != nil {
						log.Error("Failed to add ingredient: %v", err)
						bot.SendMessage(chatID, fmt.Sprintf("😢 Sorry, I couldn't add %s to your fridge.", text))
						return
					}
					
					bot.SendMessage(chatID, fmt.Sprintf("✅ Added %s to your fridge!", text))
				}
			}
		}
	}

	// Add callback handler for ingredient adding buttons
	callbackHandlers["done_adding"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID
		
		// Clear the state
		stateManager.ClearState(chatID)
		
		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Thanks! Your fridge is now updated.")
		
		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "✅ Fridge update complete! Use /fridge to see your ingredients or /dinner to get dinner suggestions.")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)
	}
	
	callbackHandlers["add_more"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID
		
		// Keep the state as is
		
		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Please send more ingredients!")
		
		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "Please send more ingredients. I'll add them to your fridge.")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("Shutting down...")
		store.Close()
		os.Exit(0)
	}()

	// Start the bot
	log.Info("Bot is now running. Press CTRL-C to exit.")
	if err := bot.Start(commandHandlers, callbackHandlers, defaultHandler); err != nil {
		log.Error("Error running bot: %v", err)
		os.Exit(1)
	}
}
