package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/korjavin/whatsfordinner/pkg/config"
	"github.com/korjavin/whatsfordinner/pkg/dinner"
	"github.com/korjavin/whatsfordinner/pkg/fridge"
	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/messages"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/openai"
	"github.com/korjavin/whatsfordinner/pkg/poll"
	"github.com/korjavin/whatsfordinner/pkg/state"
	"github.com/korjavin/whatsfordinner/pkg/stats"
	"github.com/korjavin/whatsfordinner/pkg/storage"
	"github.com/korjavin/whatsfordinner/pkg/suggest"
	"github.com/korjavin/whatsfordinner/pkg/telegram"
)

// Global map to track poll IDs to channel IDs
var pollChannelMap = make(map[string]int64)

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
	// We're not using dinnerService directly anymore, using OpenAI client instead
	// dinnerService := dinner.New(store, fridgeService, openaiClient)
	pollService := poll.New(store)
	messageService := messages.New(openaiClient)
	stateManager := state.New()
	suggestService := suggest.New(store)
	statsService := stats.New(store)

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

			// Get ingredients from the fridge
			ingredients, err := fridgeService.ListIngredients(chatID)
			if err != nil {
				log.Error("Failed to list ingredients: %v", err)
				errorMsg := messageService.GenerateErrorMessage("retrieve fridge contents")
				bot.SendMessage(chatID, errorMsg)
				return
			}

			if len(ingredients) == 0 {
				bot.SendMessage(chatID, "😢 Your fridge is empty! Please add some ingredients with /sync_fridge or /add_photo before I can suggest dinner options.")
				return
			}

			// Extract ingredient names
			ingredientNames := make([]string, len(ingredients))
			for i, ingredient := range ingredients {
				ingredientNames[i] = ingredient.Name
			}

			// Send a processing message
			processingMsg, _ := bot.SendMessage(chatID, "🧐 Thinking about dinner options based on your ingredients... This might take a moment.")

			// Get user suggestions
			userSuggestions, err := suggestService.GetUnusedSuggestions(chatID)
			if err != nil {
				log.Error("Failed to get user suggestions: %v", err)
				// Continue without user suggestions
				userSuggestions = []*models.SuggestedDish{}
			}

			// Determine how many AI suggestions to get
			aiSuggestionCount := 4
			if len(userSuggestions) > 0 {
				// If we have user suggestions, get fewer AI suggestions
				aiSuggestionCount = 4 - len(userSuggestions)
				if aiSuggestionCount < 2 {
					aiSuggestionCount = 2 // Always get at least 2 AI suggestions
				}
			}

			// Get dinner suggestions from OpenAI
			aiSuggestions, err := openaiClient.SuggestDinnerOptions(ingredientNames, cfg.Cuisines, aiSuggestionCount)
			if err != nil {
				log.Error("Failed to get dinner suggestions: %v", err)

				// If we have user suggestions, continue with those
				if len(userSuggestions) == 0 {
					bot.EditMessage(chatID, processingMsg.MessageID, "😢 Sorry, I couldn't come up with dinner suggestions right now. Please try again later.")
					return
				}

				// Continue with just user suggestions
				aiSuggestions = []map[string]interface{}{}
			}

			// Combine AI and user suggestions
			if len(aiSuggestions) == 0 && len(userSuggestions) == 0 {
				bot.EditMessage(chatID, processingMsg.MessageID, "😢 I couldn't find any suitable dishes based on your fridge contents. Try adding more ingredients with /fridge or suggest your own dishes with /suggest.")
				return
			}

			// Calculate total number of suggestions
			totalSuggestions := len(aiSuggestions) + len(userSuggestions)

			// Create options for the poll
			options := make([]string, totalSuggestions)
			dishNames := make([]string, totalSuggestions)

			// Create a detailed message with suggestions
			detailedMsg := "🍲 Here are some dinner suggestions based on your ingredients:\n\n"

			// Add user suggestions first
			for i, suggestion := range userSuggestions {
				options[i] = suggestion.Name
				dishNames[i] = fmt.Sprintf("%s (%s) - suggested by @%s", suggestion.Name, suggestion.Cuisine, suggestion.Username)

				detailedMsg += fmt.Sprintf("🍴 *%s* (%s)\n%s\n_Suggested by @%s_\n\n", suggestion.Name, suggestion.Cuisine, suggestion.Description, suggestion.Username)

				// Mark the suggestion as used
				err := suggestService.MarkAsUsed(suggestion.ID)
				if err != nil {
					log.Error("Failed to mark suggestion as used: %v", err)
				}
			}

			// Add AI suggestions
			for i, suggestion := range aiSuggestions {
				name, _ := suggestion["name"].(string)
				cuisine, _ := suggestion["cuisine"].(string)
				description, _ := suggestion["description"].(string)

				// Add to options at the correct index (after user suggestions)
				index := len(userSuggestions) + i
				options[index] = name
				dishNames[index] = fmt.Sprintf("%s (%s)", name, cuisine)

				detailedMsg += fmt.Sprintf("🍴 *%s* (%s)\n%s\n\n", name, cuisine, description)
			}

			// Edit the processing message to show the detailed suggestions
			bot.EditMessage(chatID, processingMsg.MessageID, detailedMsg)

			// Create poll
			pollMsg, err := bot.CreatePoll(chatID, "What should we cook tonight?", options)
			if err != nil {
				log.Error("Failed to create poll: %v", err)
				errorMsg := messageService.GenerateErrorMessage("create poll")
				bot.SendMessage(chatID, errorMsg)
				return
			}

			// Log the poll object to understand its structure
			log.Info("Poll message: %+v", pollMsg)
			log.Info("Poll object: %+v", pollMsg.Poll)

			// In Telegram, the poll ID we receive in poll answers is different from the poll.ID
			// We need to store the actual poll ID that will be used in poll answers
			// For now, we'll use the poll ID directly from the message
			pollID := pollMsg.Poll.ID
			log.Info("Created poll with ID %s for channel %d", pollID, chatID)
			pollChannelMap[pollID] = chatID

			// Store vote state - use the same poll ID for consistency
			_, err = pollService.CreateVote(chatID, pollID, pollMsg.MessageID, options)
			if err != nil {
				log.Error("Failed to create vote state: %v", err)
			}

			// Send a message with voting instructions
			bot.SendMessage(chatID, "🗳 Please vote for your preferred dinner option! The poll is above.")
		},
		"fridge": func(message *tgbotapi.Message) {
			// Show current ingredients
			chatID := message.Chat.ID

			ingredients, err := fridgeService.ListIngredients(chatID)
			if err != nil {
				log.Error("Failed to list ingredients: %v", err)
				bot.SendMessage(chatID, "😢 Sorry, I couldn't retrieve your fridge contents right now. Please try again later.")
				return
			}

			if len(ingredients) == 0 {
				bot.SendMessage(chatID, "Your fridge is empty! Add ingredients with /sync_fridge or by sending a photo with /add_photo.")
				return
			}

			// Create a formatted message with all ingredients
			msgText := "🧊 Here's what's in your fridge:\n\n"

			// Sort ingredients alphabetically
			sort.Slice(ingredients, func(i, j int) bool {
				return ingredients[i].Name < ingredients[j].Name
			})

			for _, ingredient := range ingredients {
				if ingredient.Quantity != "" {
					msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
				} else {
					msgText += fmt.Sprintf("• %s\n", ingredient.Name)
				}
			}

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
		"show_fridge": func(message *tgbotapi.Message) {
			// This is an alias for the /fridge command
			// Show current ingredients
			chatID := message.Chat.ID

			ingredients, err := fridgeService.ListIngredients(chatID)
			if err != nil {
				log.Error("Failed to list ingredients: %v", err)
				bot.SendMessage(chatID, "😢 Sorry, I couldn't retrieve your fridge contents right now. Please try again later.")
				return
			}

			if len(ingredients) == 0 {
				bot.SendMessage(chatID, "Your fridge is empty! Add ingredients with /sync_fridge or by sending a photo with /add_photo.")
				return
			}

			// Create a formatted message with all ingredients
			msgText := "🧊 Here's what's in your fridge:\n\n"

			// Sort ingredients alphabetically
			sort.Slice(ingredients, func(i, j int) bool {
				return ingredients[i].Name < ingredients[j].Name
			})

			for _, ingredient := range ingredients {
				if ingredient.Quantity != "" {
					msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
				} else {
					msgText += fmt.Sprintf("• %s\n", ingredient.Name)
				}
			}

			bot.SendMessage(chatID, msgText)
		},
		"add_photo": func(message *tgbotapi.Message) {
			chatID := message.Chat.ID

			// Set the chat state to adding photos
			stateManager.SetState(chatID, state.StateAddingPhotos)

			// If the message already has a photo, process it
			if message.Photo != nil && len(message.Photo) > 0 {
				// Get the largest photo (last in the array)
				photo := message.Photo[len(message.Photo)-1]

				// Send a processing message
				processingMsg, _ := bot.SendMessage(chatID, "🔍 Processing your photo... This might take a moment.")

				// Get the file URL
				photoURL, err := bot.GetFileURL(photo.FileID)
				if err != nil {
					log.Error("Failed to get photo URL: %v", err)
					bot.SendMessage(chatID, "😢 Sorry, I couldn't process your photo. Please try again.")
					return
				}

				// Extract ingredients from the photo
				ingredients, err := openaiClient.ExtractIngredientsFromPhoto(photoURL)
				if err != nil {
					log.Error("Failed to extract ingredients from photo: %v", err)
					bot.SendMessage(chatID, "😢 Sorry, I couldn't identify any ingredients in your photo. Please try again with a clearer photo.")
					return
				}

				if len(ingredients) == 0 {
					bot.SendMessage(chatID, "I couldn't identify any ingredients in your photo. Please try again with a clearer photo.")
					return
				}

				// Add ingredients to the fridge
				for _, ingredient := range ingredients {
					err := fridgeService.AddIngredient(chatID, ingredient, "")
					if err != nil {
						log.Error("Failed to add ingredient %s: %v", ingredient, err)
					}
				}

				// Edit the processing message to show the results
				bot.EditMessage(chatID, processingMsg.MessageID, fmt.Sprintf("✅ I found %d ingredients in your photo: %s", len(ingredients), strings.Join(ingredients, ", ")))

				// Ask if they want to add more photos
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Done adding photos", "done_adding_photos"),
					),
				)

				msg := tgbotapi.NewMessage(chatID, "Send more photos of your fridge or pantry, and I'll extract ingredients from them. Press 'Done' when you're finished.")
				msg.ReplyMarkup = keyboard
				bot.Send(msg)
			} else {
				// No photo in the command, instruct the user to send photos
				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Cancel", "cancel_adding_photos"),
					),
				)

				msg := tgbotapi.NewMessage(chatID, "📷 Please send photos of your fridge or pantry, and I'll extract ingredients from them. Send as many photos as you need, and I'll process each one. Press 'Cancel' if you want to stop.")
				msg.ReplyMarkup = keyboard
				bot.Send(msg)
			}
		},
		"suggest": func(message *tgbotapi.Message) {
			// Start dish suggestion flow
			chatID := message.Chat.ID
			userID := fmt.Sprintf("%d", message.From.ID)
			username := message.From.UserName
			if username == "" {
				username = message.From.FirstName
			}

			// Check if there's a dish name in the command
			args := message.CommandArguments()
			if args != "" {
				// User provided a dish name with the command
				// Send a processing message
				processingMsg, _ := bot.SendMessage(chatID, fmt.Sprintf("🧐 Looking up information about '%s'... This might take a moment.", args))

				// Get dish information from OpenAI
				dishInfo, err := openaiClient.GetDishInfo(args)
				if err != nil {
					log.Error("Failed to get dish info: %v", err)
					bot.EditMessage(chatID, processingMsg.MessageID, fmt.Sprintf("😢 Sorry, I couldn't find information about '%s'. Please try again with a different dish.", args))
					return
				}

				// Extract dish information
				dishName, _ := dishInfo["name"].(string)
				if dishName == "" {
					dishName = args // Fallback to the user-provided name
				}

				cuisine, _ := dishInfo["cuisine"].(string)
				description, _ := dishInfo["description"].(string)

				// Get ingredients needed
				var ingredientsNeeded []string
				ingredientsList, ok := dishInfo["ingredients_needed"].([]interface{})
				if !ok {
					// Try alternative key
					ingredientsList, ok = dishInfo["ingredients"].([]interface{})
				}

				if ok {
					ingredientsNeeded = make([]string, len(ingredientsList))
					for i, ing := range ingredientsList {
						if ingStr, ok := ing.(string); ok {
							ingredientsNeeded[i] = ingStr
						}
					}
				}

				// Get fridge ingredients
				fridgeIngredients, err := fridgeService.ListIngredients(chatID)
				if err != nil {
					log.Error("Failed to list ingredients: %v", err)
					// Continue without fridge comparison
					fridgeIngredients = nil
				}

				// Compare ingredients
				var missingIngredients []string
				if len(fridgeIngredients) > 0 && len(ingredientsNeeded) > 0 {
					// Extract ingredient names from fridge
					fridgeNames := make([]string, len(fridgeIngredients))
					for i, ingredient := range fridgeIngredients {
						fridgeNames[i] = ingredient.Name
					}

					// Compare ingredients
					missingIngredients = dinner.CompareIngredients(ingredientsNeeded, fridgeNames)
				}

				// Add the suggestion
				suggestion, err := suggestService.AddSuggestion(chatID, userID, username, dishName, cuisine, description)
				if err != nil {
					log.Error("Failed to add suggestion: %v", err)
					bot.EditMessage(chatID, processingMsg.MessageID, fmt.Sprintf("😢 Sorry, I couldn't save your suggestion for '%s'. Please try again later.", args))
					return
				}

				// Create a detailed message about the dish
				detailedMsg := fmt.Sprintf("✅ Thanks for suggesting *%s* (%s cuisine)!\n\n%s\n\n", suggestion.Name, suggestion.Cuisine, suggestion.Description)

				// Add ingredients information
				if len(ingredientsNeeded) > 0 {
					detailedMsg += "*Ingredients needed:*\n"
					for _, ingredient := range ingredientsNeeded {
						detailedMsg += fmt.Sprintf("• %s\n", ingredient)
					}
					detailedMsg += "\n"
				}

				// Add missing ingredients information
				if len(missingIngredients) > 0 {
					detailedMsg += "*Missing from your fridge:*\n"
					for _, ingredient := range missingIngredients {
						detailedMsg += fmt.Sprintf("• %s\n", ingredient)
					}
					detailedMsg += "\n"
				}

				detailedMsg += "Your suggestion will be included in future dinner polls."

				// Edit the processing message with the detailed information
				bot.EditMessage(chatID, processingMsg.MessageID, detailedMsg)
			} else {
				// No dish name provided, ask for it
				bot.SendMessage(chatID, "🍴 You can suggest a dish for dinner! Please use the command like this: /suggest Lasagna")
			}
		},
		"add": func(message *tgbotapi.Message) {
			// Extract ingredients from text and add them to the fridge
			chatID := message.Chat.ID

			// Check if there's text in the command
			args := message.CommandArguments()
			if args == "" {
				// No text provided, ask for it
				bot.SendMessage(chatID, "🍎 Please provide a list of ingredients to add to your fridge. For example: /add eggs, milk, bread")
				return
			}

			// Send a processing message
			processingMsg, _ := bot.SendMessage(chatID, "🔍 Processing your ingredients... This might take a moment.")

			// Parse ingredients from the text
			ingredients, err := openaiClient.ParseIngredientsFromText(args)
			if err != nil {
				log.Error("Failed to parse ingredients: %v", err)
				bot.EditMessage(chatID, processingMsg.MessageID, "😢 Sorry, I couldn't understand the ingredients. Please try again with a clearer list.")
				return
			}

			if len(ingredients) == 0 {
				bot.EditMessage(chatID, processingMsg.MessageID, "I couldn't find any ingredients in your message. Please try again with a list of ingredients.")
				return
			}

			// Add ingredients to the fridge
			for _, ingredient := range ingredients {
				err := fridgeService.AddIngredient(chatID, ingredient, "")
				if err != nil {
					log.Error("Failed to add ingredient %s: %v", ingredient, err)
				}
			}

			// Edit the processing message to show the results
			bot.EditMessage(chatID, processingMsg.MessageID, fmt.Sprintf("✅ Added %d ingredients to your fridge: %s", len(ingredients), strings.Join(ingredients, ", ")))

			// Show the updated fridge
			ingredientList, err := fridgeService.ListIngredients(chatID)
			if err != nil {
				log.Error("Failed to list ingredients: %v", err)
				return
			}

			if len(ingredientList) == 0 {
				bot.SendMessage(chatID, "Your fridge is still empty. Try adding ingredients with text or better photos.")
				return
			}

			// Create a formatted message with all ingredients
			msgText := "🧊 Here's what's in your fridge now:\n\n"

			// Sort ingredients alphabetically
			sort.Slice(ingredientList, func(i, j int) bool {
				return ingredientList[i].Name < ingredientList[j].Name
			})

			for _, ingredient := range ingredientList {
				if ingredient.Quantity != "" {
					msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
				} else {
					msgText += fmt.Sprintf("• %s\n", ingredient.Name)
				}
			}

			bot.SendMessage(chatID, msgText)
		},
		"stats": func(message *tgbotapi.Message) {
			// Show family leaderboards
			chatID := message.Chat.ID

			// Get statistics
			stats, err := statsService.GetStatistics(chatID)
			if err != nil {
				log.Error("Failed to get statistics: %v", err)
				bot.SendMessage(chatID, "😢 Sorry, I couldn't retrieve the statistics right now. Please try again later.")
				return
			}

			// Check if we have any statistics
			if len(stats.CookStats) == 0 && len(stats.HelperStats) == 0 && len(stats.SuggesterStats) == 0 {
				bot.SendMessage(chatID, "📊 No statistics available yet. Start cooking and rating meals to build up your family leaderboards!")
				return
			}

			// Create a formatted message with statistics
			msgText := "🏆 *Family Leaderboards*\n\n"

			// Add cook statistics
			if len(stats.CookStats) > 0 {
				msgText += "👨‍🍳 *Top Cooks*\n"

				// Convert map to slice for sorting
				cooks := make([]models.CookStat, 0, len(stats.CookStats))
				for _, cookStat := range stats.CookStats {
					cooks = append(cooks, cookStat)
				}

				// Sort by average rating (descending)
				sort.Slice(cooks, func(i, j int) bool {
					return cooks[i].AvgRating > cooks[j].AvgRating
				})

				// Take the top 3 cooks
				limit := 3
				if len(cooks) < limit {
					limit = len(cooks)
				}

				for i := 0; i < limit; i++ {
					cook := cooks[i]
					// Use username if available, otherwise use user ID
					displayName := cook.Username
					if displayName == "" {
						displayName = fmt.Sprintf("User %s", cook.UserID)
					}
					msgText += fmt.Sprintf("%d. %s - %.1f stars (%d meals)\n", i+1, displayName, cook.AvgRating, cook.CookCount)
				}
				msgText += "\n"
			}

			// Add helper statistics
			if len(stats.HelperStats) > 0 {
				msgText += "🛒 *Top Shoppers*\n"

				// Convert map to slice for sorting
				helpers := make([]models.HelperStat, 0, len(stats.HelperStats))
				for _, helperStat := range stats.HelperStats {
					helpers = append(helpers, helperStat)
				}

				// Sort by shopping count (descending)
				sort.Slice(helpers, func(i, j int) bool {
					return helpers[i].ShoppingCount > helpers[j].ShoppingCount
				})

				// Take the top 3 helpers
				limit := 3
				if len(helpers) < limit {
					limit = len(helpers)
				}

				for i := 0; i < limit; i++ {
					helper := helpers[i]
					// Use username if available, otherwise use user ID
					displayName := helper.Username
					if displayName == "" {
						displayName = fmt.Sprintf("User %s", helper.UserID)
					}
					msgText += fmt.Sprintf("%d. %s - %d shopping trips\n", i+1, displayName, helper.ShoppingCount)
				}
				msgText += "\n"
			}

			// Add suggester statistics
			if len(stats.SuggesterStats) > 0 {
				msgText += "💡 *Top Suggesters*\n"

				// Convert map to slice for sorting
				suggesters := make([]models.SuggesterStat, 0, len(stats.SuggesterStats))
				for _, suggesterStat := range stats.SuggesterStats {
					suggesters = append(suggesters, suggesterStat)
				}

				// Sort by acceptance rate (descending)
				sort.Slice(suggesters, func(i, j int) bool {
					// Calculate acceptance rates
					rateI := 0.0
					if suggesterStat := suggesters[i]; suggesterStat.SuggestionCount > 0 {
						rateI = float64(suggesterStat.AcceptedCount) / float64(suggesterStat.SuggestionCount)
					}

					rateJ := 0.0
					if suggesterStat := suggesters[j]; suggesterStat.SuggestionCount > 0 {
						rateJ = float64(suggesterStat.AcceptedCount) / float64(suggesterStat.SuggestionCount)
					}

					return rateI > rateJ
				})

				// Take the top 3 suggesters
				limit := 3
				if len(suggesters) < limit {
					limit = len(suggesters)
				}

				for i := 0; i < limit; i++ {
					suggester := suggesters[i]
					rate := 0.0
					if suggester.SuggestionCount > 0 {
						rate = float64(suggester.AcceptedCount) / float64(suggester.SuggestionCount) * 100
					}
					// Use username if available, otherwise use user ID
					displayName := suggester.Username
					if displayName == "" {
						displayName = fmt.Sprintf("User %s", suggester.UserID)
					}
					msgText += fmt.Sprintf("%d. %s - %.1f%% acceptance (%d/%d)\n", i+1, displayName, rate, suggester.AcceptedCount, suggester.SuggestionCount)
				}
			}

			bot.SendMessage(chatID, msgText)
		},
		// TODO: Implement other command handlers
	}

	// Setup callback handlers
	callbackHandlers := map[string]telegram.CallbackHandler{
		// TODO: Implement callback handlers
	}

	// Setup default handler
	defaultHandler := func(update tgbotapi.Update) {
		// Handle poll answers
		if update.PollAnswer != nil {
			// Get the poll ID
			pollID := update.PollAnswer.PollID
			userID := fmt.Sprintf("%d", update.PollAnswer.User.ID)

			// Debug log to see what's in our map
			log.Info("Received poll answer for poll %s from user %s", pollID, userID)
			log.Info("Poll ID type: %T, value: %v", pollID, pollID)
			for mapPollID, mapChannelID := range pollChannelMap {
				log.Info("Poll map entry: poll %s -> channel %d", mapPollID, mapChannelID)
			}

			// First check our global map for the channel ID
			var foundChannelID int64
			var exists bool

			// Check if the poll ID is in our map
			foundChannelID, exists = pollChannelMap[pollID]
			if exists {
				log.Info("Found channel %d for poll ID %s", foundChannelID, pollID)
			}

			if !exists {
				// If not in our map, try to find it using the service method
				var err error
				foundChannelID, err = pollService.FindChannelByPollID(pollID)
				if err != nil {
					log.Error("Could not find channel for poll %s: %v", pollID, err)
					return
				}
				// Add it to our map for future lookups
				pollChannelMap[pollID] = foundChannelID
				log.Info("Added poll %s to channel %d mapping from database", pollID, foundChannelID)
			}

			// Record the vote
			if len(update.PollAnswer.OptionIDs) > 0 {
				// Get the option text from the poll
				vote, err := pollService.GetVote(foundChannelID, pollID)
				if err != nil {
					log.Error("Failed to get vote: %v", err)
					return
				}

				// Get the option text
				optionID := update.PollAnswer.OptionIDs[0] // We only support single-choice polls
				if int(optionID) >= len(vote.Options) {
					log.Error("Invalid option ID: %d", optionID)
					return
				}

				option := vote.Options[optionID]

				// Record the vote
				err = pollService.RecordVote(foundChannelID, pollID, userID, option)
				if err != nil {
					log.Error("Failed to record vote: %v", err)
					return
				}

				// Get the channel state to check the member count
				channelKey := fmt.Sprintf("channel:%d", foundChannelID)
				var channelState models.ChannelState
				err = store.Get(channelKey, &channelState)
				if err != nil {
					log.Error("Failed to get channel state: %v", err)
					return
				}

				// Always get the latest member count from Telegram API
				log.Info("Attempting to get chat member count for channel %d", foundChannelID)
				chatMemberCount, err := bot.GetChatMemberCount(foundChannelID)
				if err != nil {
					log.Error("Failed to get chat member count: %v", err)
					// Fallback to default value if API call fails
					if channelState.MemberCount == 0 {
						channelState.MemberCount = 3
						log.Info("Using default member count: %d", channelState.MemberCount)
					} else {
						log.Info("Using existing member count: %d", channelState.MemberCount)
					}
				} else {
					log.Info("Got chat member count from Telegram API: %d", chatMemberCount)
					channelState.MemberCount = chatMemberCount - 1 // bot is not a family member
					// Save the updated channel state
					err = store.Set(channelKey, channelState)
					if err != nil {
						log.Error("Failed to update channel state: %v", err)
					}
				}

				// Check if we've reached the threshold to close the poll
				thresholdReached, winningOption, err := pollService.CheckVoteThreshold(foundChannelID, pollID, channelState.MemberCount, 2.0/3.0)
				if err != nil {
					log.Error("Failed to check vote threshold: %v", err)
					return
				}

				if thresholdReached {
					// End the vote
					err = pollService.EndVote(foundChannelID, pollID, winningOption)
					if err != nil {
						log.Error("Failed to end vote: %v", err)
						return
					}

					// Send a message that the poll is closed
					bot.SendMessage(foundChannelID, fmt.Sprintf("🎉 The poll has closed! The winning dish is *%s*.", winningOption))

					// Ask for cook volunteers
					keyboard := tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("I'll cook!", fmt.Sprintf("volunteer:%s", pollID)),
						),
					)

					bot.SendMessageWithKeyboard(foundChannelID, fmt.Sprintf("Who wants to cook *%s* tonight? Press the button below to volunteer!", winningOption), keyboard)
				}
			}
			return
		}

		// Skip if there's no message
		if update.Message == nil {
			return
		}

		chatID := update.Message.Chat.ID

		// Handle photos (without command)
		if len(update.Message.Photo) > 0 && !update.Message.IsCommand() {
			// Check if the chat is in adding ingredients state
			chatState := stateManager.GetState(chatID)
			if chatState == state.StateAddingIngredients || chatState == state.StateAddingPhotos {
				// Get the largest photo (last in the array)
				photo := update.Message.Photo[len(update.Message.Photo)-1]

				// Send a processing message
				processingMsg, _ := bot.SendMessage(chatID, "🔍 Processing your photo... This might take a moment.")

				// Get the file URL
				photoURL, err := bot.GetFileURL(photo.FileID)
				if err != nil {
					log.Error("Failed to get photo URL: %v", err)
					bot.SendMessage(chatID, "😢 Sorry, I couldn't process your photo. Please try again.")
					return
				}

				// Extract ingredients from the photo
				ingredients, err := openaiClient.ExtractIngredientsFromPhoto(photoURL)
				if err != nil {
					log.Error("Failed to extract ingredients from photo: %v", err)
					bot.SendMessage(chatID, "😢 Sorry, I couldn't identify any ingredients in your photo. Please try again with a clearer photo.")
					return
				}

				if len(ingredients) == 0 {
					bot.SendMessage(chatID, "I couldn't identify any ingredients in your photo. Please try again with a clearer photo.")
					return
				}

				// Add ingredients to the fridge
				for _, ingredient := range ingredients {
					err := fridgeService.AddIngredient(chatID, ingredient, "")
					if err != nil {
						log.Error("Failed to add ingredient %s: %v", ingredient, err)
					}
				}

				// Edit the processing message to show the results
				bot.EditMessage(chatID, processingMsg.MessageID, fmt.Sprintf("✅ I found %d ingredients in your photo: %s", len(ingredients), strings.Join(ingredients, ", ")))

				// Different buttons based on the state
				var keyboard tgbotapi.InlineKeyboardMarkup
				var promptText string

				if chatState == state.StateAddingIngredients {
					// For text-based ingredient adding
					keyboard = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("Done adding ingredients", "done_adding"),
							tgbotapi.NewInlineKeyboardButtonData("Add more", "add_more"),
						),
					)
					promptText = "Would you like to add more ingredients or are you done?"
				} else {
					// For photo-based ingredient adding
					keyboard = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("Done adding photos", "done_adding_photos"),
						),
					)
					promptText = "Send more photos of your fridge or pantry, and I'll extract ingredients from them. Press 'Done' when you're finished."
				}

				msg := tgbotapi.NewMessage(chatID, promptText)
				msg.ReplyMarkup = keyboard
				bot.Send(msg)
			} else {
				// Suggest using /add_photo command
				bot.SendMessage(chatID, "I see you sent a photo! If you want me to extract ingredients from it, please use the /add_photo command.")
			}
			return
		}

		// Handle text messages
		if update.Message.Text != "" && !update.Message.IsCommand() {
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
			} else if stateManager.GetState(chatID) == state.StateSuggestingDish {
				// We're now handling this directly in the /suggest command
				// Just clear the state and ask the user to use the command
				stateManager.ClearState(chatID)
				bot.SendMessage(chatID, "🍴 Please use the /suggest command followed by a dish name, like: /suggest Lasagna")
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

	callbackHandlers["show_fridge"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Here's what's in your fridge!")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "Here's what's in your fridge:")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Show fridge contents
		ingredients, err := fridgeService.ListIngredients(chatID)
		if err != nil {
			log.Error("Failed to list ingredients: %v", err)
			bot.SendMessage(chatID, "😢 Sorry, I couldn't retrieve your fridge contents right now. Please try again later.")
			return
		}

		if len(ingredients) == 0 {
			bot.SendMessage(chatID, "Your fridge is empty! Add ingredients with /sync_fridge or by sending a photo with /add_photo.")
			return
		}

		// Create a formatted message with all ingredients
		msgText := "🧊 Here's what's in your fridge:\n\n"

		// Sort ingredients alphabetically
		sort.Slice(ingredients, func(i, j int) bool {
			return ingredients[i].Name < ingredients[j].Name
		})

		for _, ingredient := range ingredients {
			if ingredient.Quantity != "" {
				msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
			} else {
				msgText += fmt.Sprintf("• %s\n", ingredient.Name)
			}
		}

		bot.SendMessage(chatID, msgText)
	}

	callbackHandlers["done_adding_photos"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID

		// Clear the state
		stateManager.ClearState(chatID)

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Thanks! Your fridge is now updated with ingredients from your photos.")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "✅ Photo processing complete! I've added all the ingredients I found to your fridge.")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Show fridge contents
		ingredients, err := fridgeService.ListIngredients(chatID)
		if err != nil {
			log.Error("Failed to list ingredients: %v", err)
			return
		}

		if len(ingredients) == 0 {
			bot.SendMessage(chatID, "Your fridge is still empty. Try adding ingredients with text or better photos.")
			return
		}

		// Create a formatted message with all ingredients
		msgText := "🧊 Here's what's in your fridge:\n\n"

		// Sort ingredients alphabetically
		sort.Slice(ingredients, func(i, j int) bool {
			return ingredients[i].Name < ingredients[j].Name
		})

		for _, ingredient := range ingredients {
			if ingredient.Quantity != "" {
				msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
			} else {
				msgText += fmt.Sprintf("• %s\n", ingredient.Name)
			}
		}

		bot.SendMessage(chatID, msgText)

		// Suggest next steps
		bot.SendMessage(chatID, "You can now use /dinner to get dinner suggestions based on your ingredients!")
	}

	callbackHandlers["cancel_adding_photos"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID

		// Clear the state
		stateManager.ClearState(chatID)

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Photo adding cancelled.")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "Photo adding cancelled. You can use /fridge to see your current ingredients or /dinner to get dinner suggestions.")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)
	}

	// Handle volunteer for cooking
	callbackHandlers["volunteer:"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID
		userID := fmt.Sprintf("%d", callback.From.ID)
		username := callback.From.UserName
		if username == "" {
			username = callback.From.FirstName
		}

		// Extract the poll ID from the callback data
		parts := strings.Split(callback.Data, ":")
		if len(parts) != 2 {
			log.Error("Invalid callback data: %s", callback.Data)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		pollID := parts[1]

		// Add the volunteer
		err := pollService.AddCookVolunteer(chatID, pollID, userID)
		if err != nil {
			log.Error("Failed to add cook volunteer: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Thanks for volunteering to cook!")

		// Get the vote to find the winning dish
		vote, err := pollService.GetVote(chatID, pollID)
		if err != nil {
			log.Error("Failed to get vote: %v", err)
			return
		}

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, fmt.Sprintf("@%s has volunteered to cook %s tonight!", username, vote.WinningDish))
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Get dish information from OpenAI
		dishInfo, err := openaiClient.GetDishInfo(vote.WinningDish)
		if err != nil {
			log.Error("Failed to get dish info: %v", err)
			bot.SendMessage(chatID, fmt.Sprintf("😢 Sorry, I couldn't find cooking instructions for %s. @%s, you're on your own for this one!", vote.WinningDish, username))
			return
		}

		// Extract dish information
		dishName, _ := dishInfo["name"].(string)
		if dishName == "" {
			dishName = vote.WinningDish // Fallback to the winning dish name
		}

		// Get ingredients needed
		var ingredientsNeeded []string
		ingredientsList, ok := dishInfo["ingredients_needed"].([]interface{})
		if !ok {
			// Try alternative key
			ingredientsList, ok = dishInfo["ingredients"].([]interface{})
		}

		if ok {
			ingredientsNeeded = make([]string, len(ingredientsList))
			for i, ing := range ingredientsList {
				if ingStr, ok := ing.(string); ok {
					ingredientsNeeded[i] = ingStr
				}
			}
		}

		// Get instructions
		var instructions []string
		instructionsList, ok := dishInfo["instructions"].([]interface{})
		if ok {
			instructions = make([]string, len(instructionsList))
			for i, inst := range instructionsList {
				if instStr, ok := inst.(string); ok {
					instructions[i] = instStr
				}
			}
		}

		// Create a dish object
		dish := models.Dish{
			Name:         dishName,
			Cuisine:      vote.WinningDish, // We don't have the cuisine, so use the dish name
			Ingredients:  ingredientsNeeded,
			Instructions: instructions,
		}

		// Create a dinner event
		dinnerService := dinner.New(store, fridgeService, openaiClient)
		dinnerEvent, err := dinnerService.CreateDinner(chatID, dish, userID)
		if err != nil {
			log.Error("Failed to create dinner event: %v", err)
			// Continue anyway
		}

		// Update cook statistics with the username
		err = statsService.UpdateCookStats(chatID, userID, username, 0) // No rating yet
		if err != nil {
			log.Error("Failed to update cook stats: %v", err)
			// Continue anyway
		}

		// Update suggester statistics if this was a user-suggested dish
		// We would need to check if the dish was suggested by a user and update their stats
		// For now, we'll just update the cook's stats when the dinner is rated

		// Send cooking instructions
		msgText := fmt.Sprintf("🍳 *Cooking Instructions for %s*\n\n", dishName)

		// Add ingredients
		if len(ingredientsNeeded) > 0 {
			msgText += "*Ingredients:*\n"
			for _, ingredient := range ingredientsNeeded {
				msgText += fmt.Sprintf("• %s\n", ingredient)
			}
			msgText += "\n"
		}

		// Add instructions
		if len(instructions) > 0 {
			msgText += "*Instructions:*\n"
			for i, instruction := range instructions {
				msgText += fmt.Sprintf("%d. %s\n", i+1, instruction)
			}
		}

		// Add cooking status buttons
		callbackData := fmt.Sprintf("dinner_ready:%s", dinnerEvent.ID)
		log.Info("Creating 'Dinner is ready' button with callback data: %s", callbackData)
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🍽️ Dinner is ready!", callbackData),
			),
		)

		bot.SendMessageWithKeyboard(chatID, msgText, keyboard)
	}

	// Handle dinner ready callback
	callbackHandlers["dinner_ready:"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID
		userID := fmt.Sprintf("%d", callback.From.ID)
		username := callback.From.UserName
		if username == "" {
			username = callback.From.FirstName
		}

		// Extract the dinner ID from the callback data
		// The format is "dinner_ready:dinner:{channelID}:{timestamp}"
		parts := strings.Split(callback.Data, ":")
		if len(parts) < 2 {
			log.Error("Invalid callback data: %s", callback.Data)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Reconstruct the dinner ID by joining all parts after "dinner_ready"
		dinnerID := strings.Join(parts[1:], ":")

		// Get the dinner event
		log.Info("Looking up dinner with ID: %s", dinnerID)
		var dinnerEvent models.Dinner
		err := store.Get(dinnerID, &dinnerEvent)
		if err != nil {
			log.Error("Failed to get dinner event: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}
		log.Info("Successfully found dinner: %s cooked by %s", dinnerEvent.Dish.Name, dinnerEvent.Cook)

		// Check if the user is the cook
		if dinnerEvent.Cook != userID {
			bot.AnswerCallbackQuery(callback.ID, "Only the cook can mark dinner as ready.")
			return
		}

		// Mark the dinner as finished
		dinnerService := dinner.New(store, fridgeService, openaiClient)
		err = dinnerService.FinishDinner(chatID)
		if err != nil {
			log.Error("Failed to finish dinner: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Dinner is ready!")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, callback.Message.Text+"\n\n✅ Dinner is ready!")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Send a message to the chat
		bot.SendMessage(chatID, fmt.Sprintf("🍽️ *Dinner is ready!* @%s has prepared %s. Enjoy your meal!", username, dinnerEvent.Dish.Name))

		// Add rating buttons
		log.Info("Creating rating buttons for dinner ID: %s", dinnerID)

		// Create callback data for each rating
		rate1 := fmt.Sprintf("rate:%s:1", dinnerID)
		rate2 := fmt.Sprintf("rate:%s:2", dinnerID)
		rate3 := fmt.Sprintf("rate:%s:3", dinnerID)
		rate4 := fmt.Sprintf("rate:%s:4", dinnerID)
		rate5 := fmt.Sprintf("rate:%s:5", dinnerID)

		log.Info("Rating callback example: %s", rate3) // Log one example for debugging

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⭐", rate1),
				tgbotapi.NewInlineKeyboardButtonData("⭐⭐", rate2),
				tgbotapi.NewInlineKeyboardButtonData("⭐⭐⭐", rate3),
				tgbotapi.NewInlineKeyboardButtonData("⭐⭐⭐⭐", rate4),
				tgbotapi.NewInlineKeyboardButtonData("⭐⭐⭐⭐⭐", rate5),
			),
		)

		bot.SendMessageWithKeyboard(chatID, "How would you rate tonight's dinner? Your feedback helps improve future suggestions!", keyboard)
	}

	// Handle dinner rating callback
	callbackHandlers["rate:"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID
		userID := fmt.Sprintf("%d", callback.From.ID)
		username := callback.From.UserName
		if username == "" {
			username = callback.From.FirstName
		}

		// Extract the dinner ID and rating from the callback data
		// The format is "rate:dinner:{channelID}:{timestamp}:{rating}"
		parts := strings.Split(callback.Data, ":")
		if len(parts) < 3 {
			log.Error("Invalid callback data: %s", callback.Data)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// The last part is the rating
		rating, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil || rating < 1 || rating > 5 {
			log.Error("Invalid rating: %s", parts[len(parts)-1])
			bot.AnswerCallbackQuery(callback.ID, "Invalid rating. Please try again.")
			return
		}

		// Reconstruct the dinner ID by joining all parts between "rate" and the rating
		dinnerID := strings.Join(parts[1:len(parts)-1], ":")
		log.Info("Looking up dinner with ID: %s for rating: %d", dinnerID, rating)

		// Get the dinner event
		var dinnerEvent models.Dinner
		err = store.Get(dinnerID, &dinnerEvent)
		if err != nil {
			log.Error("Failed to get dinner event: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Add the rating
		dinnerService := dinner.New(store, fridgeService, openaiClient)
		err = dinnerService.RateDinner(dinnerID, userID, rating)
		if err != nil {
			log.Error("Failed to rate dinner: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Update cook statistics
		cookID := dinnerEvent.Cook

		// Try to get the cook's username from the chat members
		cookUsername := ""
		// First check if the current user is the cook
		if cookID == userID {
			cookUsername = username
		}

		// Update cook statistics
		err = statsService.UpdateCookStats(chatID, cookID, cookUsername, float64(rating))
		if err != nil {
			log.Error("Failed to update cook stats: %v", err)
			// Continue anyway
		}

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, fmt.Sprintf("Thanks for rating %d stars!", rating))

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, fmt.Sprintf("Thanks for your feedback! @%s rated tonight's dinner %d stars.", username, rating))
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Update the fridge by removing used ingredients
		if len(dinnerEvent.Dish.Ingredients) > 0 {
			// Ask if they want to update the fridge
			log.Info("Creating update fridge buttons for dinner ID: %s", dinnerID)

			// Create callback data for update fridge
			updateFridgeCallback := fmt.Sprintf("update_fridge:%s", dinnerID)
			log.Info("Update fridge callback: %s", updateFridgeCallback)

			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Yes, update fridge", updateFridgeCallback),
					tgbotapi.NewInlineKeyboardButtonData("No, keep as is", "skip_update_fridge"),
				),
			)

			bot.SendMessageWithKeyboard(chatID, "Would you like to update your fridge by removing the ingredients used for this dinner?", keyboard)
		}
	}

	// Handle update fridge callback
	callbackHandlers["update_fridge"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID

		// Extract the dinner ID from the callback data
		// The format is "update_fridge:dinner:{channelID}:{timestamp}"
		parts := strings.Split(callback.Data, ":")
		if len(parts) < 2 {
			log.Error("Invalid callback data: %s", callback.Data)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Reconstruct the dinner ID by joining all parts after "update_fridge"
		dinnerID := strings.Join(parts[1:], ":")
		log.Info("Extracted dinner ID: %s from callback data: %s", dinnerID, callback.Data)
		log.Info("Looking up dinner with ID: %s for fridge update", dinnerID)

		// Get the dinner event
		var dinnerEvent models.Dinner
		err := store.Get(dinnerID, &dinnerEvent)
		if err != nil {
			log.Error("Failed to get dinner event: %v", err)
			bot.AnswerCallbackQuery(callback.ID, "Something went wrong. Please try again.")
			return
		}

		// Remove ingredients from the fridge
		for _, ingredient := range dinnerEvent.Dish.Ingredients {
			err := fridgeService.RemoveIngredient(chatID, ingredient)
			if err != nil {
				log.Error("Failed to remove ingredient %s: %v", ingredient, err)
				// Continue with other ingredients
			}
		}

		// Update the dinner with the used ingredients
		dinnerService := dinner.New(store, fridgeService, openaiClient)
		err = dinnerService.UpdateUsedIngredients(dinnerID, dinnerEvent.Dish.Ingredients)
		if err != nil {
			log.Error("Failed to update used ingredients: %v", err)
			// Continue anyway
		}

		// Update helper statistics (for future shopping feature)
		// For now, we'll just acknowledge the callback

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Fridge updated!")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "✅ Your fridge has been updated by removing the ingredients used for this dinner.")
		editMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
		bot.Send(editMsg)

		// Show the updated fridge
		ingredients, err := fridgeService.ListIngredients(chatID)
		if err != nil {
			log.Error("Failed to list ingredients: %v", err)
			return
		}

		if len(ingredients) == 0 {
			bot.SendMessage(chatID, "Your fridge is now empty! You might want to add more ingredients with /sync_fridge or /add_photo.")
			return
		}

		// Create a formatted message with all ingredients
		msgText := "🧊 Here's what's left in your fridge:\n\n"

		// Sort ingredients alphabetically
		sort.Slice(ingredients, func(i, j int) bool {
			return ingredients[i].Name < ingredients[j].Name
		})

		for _, ingredient := range ingredients {
			if ingredient.Quantity != "" {
				msgText += fmt.Sprintf("• %s (%s)\n", ingredient.Name, ingredient.Quantity)
			} else {
				msgText += fmt.Sprintf("• %s\n", ingredient.Name)
			}
		}

		bot.SendMessage(chatID, msgText)
	}

	// Handle skip update fridge callback
	callbackHandlers["skip_update_fridge"] = func(callback *tgbotapi.CallbackQuery) {
		chatID := callback.Message.Chat.ID

		// Answer the callback
		bot.AnswerCallbackQuery(callback.ID, "Fridge not updated.")

		// Edit the message to remove the buttons
		editMsg := tgbotapi.NewEditMessageText(chatID, callback.Message.MessageID, "Fridge not updated. Your ingredients remain the same.")
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
