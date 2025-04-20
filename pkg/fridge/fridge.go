package fridge

import (
	"fmt"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/storage"
)

// Service provides fridge management functionality
type Service struct {
	store  *storage.Store
	logger *logger.Logger
}

// New creates a new fridge service
func New(store *storage.Store) *Service {
	return &Service{
		store:  store,
		logger: logger.New(""),
	}
}

// GetFridge retrieves the fridge for a channel
func (s *Service) GetFridge(channelID int64) (*models.Fridge, error) {
	fridgeKey := fmt.Sprintf("fridge:%d", channelID)
	
	var fridge models.Fridge
	err := s.store.Get(fridgeKey, &fridge)
	if err != nil {
		// If the fridge doesn't exist, create a new one
		fridge = models.Fridge{
			ID:          fridgeKey,
			ChannelID:   channelID,
			Ingredients: make(map[string]models.Ingredient),
			LastUpdated: time.Now(),
		}
		
		if err := s.store.Set(fridgeKey, fridge); err != nil {
			return nil, fmt.Errorf("failed to create fridge: %w", err)
		}
	}
	
	return &fridge, nil
}

// AddIngredient adds an ingredient to the fridge
func (s *Service) AddIngredient(channelID int64, name, quantity string) error {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return err
	}
	
	fridge.Ingredients[name] = models.Ingredient{
		Name:     name,
		Quantity: quantity,
		AddedAt:  time.Now(),
	}
	
	fridge.LastUpdated = time.Now()
	
	return s.store.Set(fridge.ID, fridge)
}

// RemoveIngredient removes an ingredient from the fridge
func (s *Service) RemoveIngredient(channelID int64, name string) error {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return err
	}
	
	delete(fridge.Ingredients, name)
	fridge.LastUpdated = time.Now()
	
	return s.store.Set(fridge.ID, fridge)
}

// ListIngredients returns a list of all ingredients in the fridge
func (s *Service) ListIngredients(channelID int64) ([]models.Ingredient, error) {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return nil, err
	}
	
	ingredients := make([]models.Ingredient, 0, len(fridge.Ingredients))
	for _, ingredient := range fridge.Ingredients {
		ingredients = append(ingredients, ingredient)
	}
	
	return ingredients, nil
}

// HasIngredients checks if the fridge has all the specified ingredients
func (s *Service) HasIngredients(channelID int64, ingredientNames []string) (bool, []string, error) {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return false, nil, err
	}
	
	missing := make([]string, 0)
	for _, name := range ingredientNames {
		if _, ok := fridge.Ingredients[name]; !ok {
			missing = append(missing, name)
		}
	}
	
	return len(missing) == 0, missing, nil
}

// ResetFridge resets the fridge for a channel
func (s *Service) ResetFridge(channelID int64) error {
	fridgeKey := fmt.Sprintf("fridge:%d", channelID)
	
	fridge := models.Fridge{
		ID:          fridgeKey,
		ChannelID:   channelID,
		Ingredients: make(map[string]models.Ingredient),
		LastUpdated: time.Now(),
	}
	
	return s.store.Set(fridgeKey, fridge)
}

// UpdateIngredients updates multiple ingredients at once
func (s *Service) UpdateIngredients(channelID int64, ingredients map[string]string) error {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return err
	}
	
	for name, quantity := range ingredients {
		fridge.Ingredients[name] = models.Ingredient{
			Name:     name,
			Quantity: quantity,
			AddedAt:  time.Now(),
		}
	}
	
	fridge.LastUpdated = time.Now()
	
	return s.store.Set(fridge.ID, fridge)
}

// RemoveIngredients removes multiple ingredients at once
func (s *Service) RemoveIngredients(channelID int64, ingredientNames []string) error {
	fridge, err := s.GetFridge(channelID)
	if err != nil {
		return err
	}
	
	for _, name := range ingredientNames {
		delete(fridge.Ingredients, name)
	}
	
	fridge.LastUpdated = time.Now()
	
	return s.store.Set(fridge.ID, fridge)
}
