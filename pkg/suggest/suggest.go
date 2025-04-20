package suggest

import (
	"fmt"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/storage"
)

// Service provides functionality for managing suggested dishes
type Service struct {
	store  *storage.Store
	logger *logger.Logger
}

// New creates a new suggest service
func New(store *storage.Store) *Service {
	return &Service{
		store:  store,
		logger: logger.New(""),
	}
}

// AddSuggestion adds a new suggested dish
func (s *Service) AddSuggestion(channelID int64, userID, username, name, cuisine, description string) (*models.SuggestedDish, error) {
	s.logger.Info("Adding suggestion from user %s: %s (%s cuisine)", username, name, cuisine)
	
	suggestion := &models.SuggestedDish{
		ID:          fmt.Sprintf("suggestion:%d:%d", channelID, time.Now().UnixNano()),
		ChannelID:   channelID,
		UserID:      userID,
		Username:    username,
		Name:        name,
		Cuisine:     cuisine,
		Description: description,
		SuggestedAt: time.Now(),
		UsedInPoll:  false,
	}
	
	err := s.store.Set(suggestion.ID, suggestion)
	if err != nil {
		s.logger.Error("Failed to save suggestion: %v", err)
		return nil, fmt.Errorf("failed to save suggestion: %w", err)
	}
	
	s.logger.Info("Successfully added suggestion %s", suggestion.ID)
	return suggestion, nil
}

// GetSuggestions returns all suggestions for a channel
func (s *Service) GetSuggestions(channelID int64) ([]*models.SuggestedDish, error) {
	keys, err := s.store.List(fmt.Sprintf("suggestion:%d:", channelID))
	if err != nil {
		return nil, fmt.Errorf("failed to list suggestions: %w", err)
	}
	
	suggestions := make([]*models.SuggestedDish, 0, len(keys))
	for _, key := range keys {
		var suggestion models.SuggestedDish
		err := s.store.Get(key, &suggestion)
		if err != nil {
			s.logger.Error("Failed to get suggestion %s: %v", key, err)
			continue
		}
		
		suggestions = append(suggestions, &suggestion)
	}
	
	return suggestions, nil
}

// GetUnusedSuggestions returns all suggestions that haven't been used in a poll
func (s *Service) GetUnusedSuggestions(channelID int64) ([]*models.SuggestedDish, error) {
	suggestions, err := s.GetSuggestions(channelID)
	if err != nil {
		return nil, err
	}
	
	unused := make([]*models.SuggestedDish, 0)
	for _, suggestion := range suggestions {
		if !suggestion.UsedInPoll {
			unused = append(unused, suggestion)
		}
	}
	
	return unused, nil
}

// MarkAsUsed marks a suggestion as used in a poll
func (s *Service) MarkAsUsed(suggestionID string) error {
	var suggestion models.SuggestedDish
	err := s.store.Get(suggestionID, &suggestion)
	if err != nil {
		return fmt.Errorf("failed to get suggestion: %w", err)
	}
	
	suggestion.UsedInPoll = true
	
	err = s.store.Set(suggestionID, suggestion)
	if err != nil {
		return fmt.Errorf("failed to update suggestion: %w", err)
	}
	
	return nil
}

// DeleteSuggestion deletes a suggestion
func (s *Service) DeleteSuggestion(suggestionID string) error {
	return s.store.Delete(suggestionID)
}
