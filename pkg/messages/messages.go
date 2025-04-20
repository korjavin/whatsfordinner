package messages

import (
	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/openai"
)

// Service provides message generation functionality
type Service struct {
	openaiClient *openai.Client
	logger       *logger.Logger
}

// New creates a new message service
func New(openaiClient *openai.Client) *Service {
	return &Service{
		openaiClient: openaiClient,
		logger:       logger.New(""),
	}
}

// GenerateWelcomeMessage generates a welcome message
func (s *Service) GenerateWelcomeMessage() string {
	msg, err := s.openaiClient.GenerateChatMessage("welcome", map[string]interface{}{
		"purpose": "Help families decide what to cook for dinner",
	})
	if err != nil {
		s.logger.Error("Failed to generate welcome message: %v", err)
		return "👋 Welcome to WhatsForDinner bot! I'll help your family decide what to cook for dinner."
	}
	return msg
}

// GenerateDinnerSuggestions generates a message with dinner suggestions
func (s *Service) GenerateDinnerSuggestions(dishes []string) string {
	msg, err := s.openaiClient.GenerateChatMessage("dinner_suggestions", map[string]interface{}{
		"dishes": dishes,
	})
	if err != nil {
		s.logger.Error("Failed to generate dinner suggestions message: %v", err)
		return "🍽️ Hey family! It's dinner time! Based on what we have, here are some ideas:\n" + formatDishes(dishes)
	}
	return msg
}

// GenerateEmptyFridgeMessage generates a message for an empty fridge
func (s *Service) GenerateEmptyFridgeMessage() string {
	msg, err := s.openaiClient.GenerateChatMessage("empty_fridge", map[string]interface{}{})
	if err != nil {
		s.logger.Error("Failed to generate empty fridge message: %v", err)
		return "Your fridge is empty! Add ingredients with /sync_fridge or by sending a photo with /add_photo."
	}
	return msg
}

// GenerateFridgeContentsMessage generates a message with fridge contents
func (s *Service) GenerateFridgeContentsMessage(ingredients []string) string {
	msg, err := s.openaiClient.GenerateChatMessage("fridge_contents", map[string]interface{}{
		"ingredients": ingredients,
	})
	if err != nil {
		s.logger.Error("Failed to generate fridge contents message: %v", err)
		return "🧊 Here's what's in your fridge:\n" + formatIngredients(ingredients)
	}
	return msg
}

// GenerateErrorMessage generates an error message
func (s *Service) GenerateErrorMessage(context string) string {
	msg, err := s.openaiClient.GenerateChatMessage("error", map[string]interface{}{
		"context": context,
	})
	if err != nil {
		s.logger.Error("Failed to generate error message: %v", err)
		return "😢 Sorry, something went wrong. Please try again later."
	}
	return msg
}

// GenerateCookVolunteerRequest generates a message asking for cook volunteers
func (s *Service) GenerateCookVolunteerRequest(dish string) string {
	msg, err := s.openaiClient.GenerateChatMessage("cook_volunteer_request", map[string]interface{}{
		"dish": dish,
	})
	if err != nil {
		s.logger.Error("Failed to generate cook volunteer request message: %v", err)
		return "✅ " + dish + " wins! Now, who wants to cook it?"
	}
	return msg
}

// GenerateCookConfirmation generates a message confirming the cook
func (s *Service) GenerateCookConfirmation(cook, dish string) string {
	msg, err := s.openaiClient.GenerateChatMessage("cook_confirmation", map[string]interface{}{
		"cook": cook,
		"dish": dish,
	})
	if err != nil {
		s.logger.Error("Failed to generate cook confirmation message: %v", err)
		return "👨‍🍳 Great! @" + cook + " is the chef tonight."
	}
	return msg
}

// Helper functions for fallback formatting
func formatDishes(dishes []string) string {
	result := ""
	for i, dish := range dishes {
		result += string(rune('1'+i)) + ". " + dish + "\n"
	}
	return result
}

func formatIngredients(ingredients []string) string {
	result := "🧊 Here's what's in your fridge:\n\n"
	for _, ingredient := range ingredients {
		result += "• " + ingredient + "\n"
	}
	return result
}
