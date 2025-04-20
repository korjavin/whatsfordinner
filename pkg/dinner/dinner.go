package dinner

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/fridge"
	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/openai"
	"github.com/korjavin/whatsfordinner/pkg/storage"
)

// Service provides dinner planning functionality
type Service struct {
	store         *storage.Store
	fridgeService *fridge.Service
	openaiClient  *openai.Client
	logger        *logger.Logger
}

// New creates a new dinner service
func New(store *storage.Store, fridgeService *fridge.Service, openaiClient *openai.Client) *Service {
	return &Service{
		store:         store,
		fridgeService: fridgeService,
		openaiClient:  openaiClient,
		logger:        logger.New(""),
	}
}

// GetDishes returns a list of all available dishes
func (s *Service) GetDishes() ([]models.Dish, error) {
	// Get dishes from the database
	dishKeys, err := s.store.List("dish:")
	if err != nil {
		return nil, fmt.Errorf("failed to list dishes: %w", err)
	}

	// If we have dishes in the database, return them
	if len(dishKeys) > 0 {
		dishes := make([]models.Dish, 0, len(dishKeys))
		for _, key := range dishKeys {
			var dish models.Dish
			err := s.store.Get(key, &dish)
			if err != nil {
				s.logger.Error("Failed to get dish %s: %v", key, err)
				continue
			}
			dishes = append(dishes, dish)
		}
		return dishes, nil
	}

	// If we don't have dishes in the database, create some default ones using OpenAI
	defaultDishes := []struct {
		Name    string
		Cuisine string
	}{
		{"Spaghetti Bolognese", "Italian"},
		{"Borscht", "Russian"},
		{"Chicken Alfredo", "Italian"},
		{"Meatballs with Potatoes", "European"},
		{"Beef Stroganoff", "Russian"},
	}

	dishes := make([]models.Dish, 0, len(defaultDishes))
	for _, defaultDish := range defaultDishes {
		// Get dish info from OpenAI
		dishInfo, err := s.openaiClient.GetDishInfo(defaultDish.Name, defaultDish.Cuisine)
		if err != nil {
			s.logger.Error("Failed to get dish info for %s: %v", defaultDish.Name, err)
			continue
		}

		// Extract ingredients and instructions
		ingredients, _ := dishInfo["ingredients"].([]interface{})
		instructions, _ := dishInfo["instructions"].([]interface{})

		// Convert to string slices
		ingredientStrs := make([]string, 0, len(ingredients))
		for _, ingredient := range ingredients {
			if ingStr, ok := ingredient.(string); ok {
				ingredientStrs = append(ingredientStrs, ingStr)
			}
		}

		instructionStrs := make([]string, 0, len(instructions))
		for _, instruction := range instructions {
			if instStr, ok := instruction.(string); ok {
				instructionStrs = append(instructionStrs, instStr)
			}
		}

		// Create dish
		dish := models.Dish{
			Name:         defaultDish.Name,
			Cuisine:      defaultDish.Cuisine,
			Ingredients:  ingredientStrs,
			Instructions: instructionStrs,
		}

		// Save dish to database
		dishKey := fmt.Sprintf("dish:%s:%s", defaultDish.Cuisine, defaultDish.Name)
		err = s.store.Set(dishKey, dish)
		if err != nil {
			s.logger.Error("Failed to save dish %s: %v", dishKey, err)
		}

		dishes = append(dishes, dish)
	}

	return dishes, nil
}

// SuggestDishes suggests dishes based on available ingredients and cuisine preferences
func (s *Service) SuggestDishes(channelID int64, cuisines []string, count int) ([]models.Dish, error) {
	allDishes, err := s.GetDishes()
	if err != nil {
		return nil, err
	}

	// Filter dishes by cuisine
	var filteredDishes []models.Dish
	if len(cuisines) > 0 {
		for _, dish := range allDishes {
			for _, cuisine := range cuisines {
				if dish.Cuisine == cuisine {
					filteredDishes = append(filteredDishes, dish)
					break
				}
			}
		}
	} else {
		filteredDishes = allDishes
	}

	// If no dishes match the cuisines, fall back to all dishes
	if len(filteredDishes) == 0 {
		filteredDishes = allDishes
		s.logger.Warn("No dishes found for cuisines %v, falling back to all dishes", cuisines)
	}

	// Get available ingredients
	ingredients, err := s.fridgeService.ListIngredients(channelID)
	if err != nil {
		return nil, err
	}

	// Create a map of available ingredients for faster lookup
	availableIngredients := make(map[string]bool)
	for _, ingredient := range ingredients {
		availableIngredients[ingredient.Name] = true
	}

	// Score dishes based on available ingredients
	type scoredDish struct {
		dish  models.Dish
		score float64
	}

	var scoredDishes []scoredDish
	for _, dish := range filteredDishes {
		var matchCount int
		for _, ingredient := range dish.Ingredients {
			if availableIngredients[ingredient] {
				matchCount++
			}
		}

		// Calculate score as percentage of matching ingredients
		score := float64(matchCount) / float64(len(dish.Ingredients))
		scoredDishes = append(scoredDishes, scoredDish{dish, score})
	}

	// Sort dishes by score (descending)
	// Use a local random source instead of the deprecated rand.Seed
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(scoredDishes), func(i, j int) {
		scoredDishes[i], scoredDishes[j] = scoredDishes[j], scoredDishes[i]
	})

	// Take the top N dishes
	result := make([]models.Dish, 0, count)
	for i := 0; i < len(scoredDishes) && i < count; i++ {
		result = append(result, scoredDishes[i].dish)
	}

	return result, nil
}

// CreateDinner creates a new dinner event
func (s *Service) CreateDinner(channelID int64, dish models.Dish, cook string) (*models.Dinner, error) {
	dinner := &models.Dinner{
		ID:        fmt.Sprintf("dinner:%d:%d", channelID, time.Now().Unix()),
		ChannelID: channelID,
		Dish:      dish,
		Cook:      cook,
		StartedAt: time.Now(),
		Ratings:   make(map[string]int),
	}

	err := s.store.Set(dinner.ID, dinner)
	if err != nil {
		return nil, err
	}

	// Update channel state
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err = s.store.Get(channelKey, &channelState)
	if err != nil {
		// Create new channel state if it doesn't exist
		channelState = models.ChannelState{
			ChannelID:    channelID,
			FridgeID:     fmt.Sprintf("fridge:%d", channelID),
			LastActivity: time.Now(),
		}
	}

	channelState.CurrentDinner = dinner
	channelState.LastActivity = time.Now()

	err = s.store.Set(channelKey, channelState)
	if err != nil {
		return nil, err
	}

	return dinner, nil
}

// FinishDinner marks a dinner as finished
func (s *Service) FinishDinner(channelID int64) error {
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err := s.store.Get(channelKey, &channelState)
	if err != nil {
		return err
	}

	if channelState.CurrentDinner == nil {
		return fmt.Errorf("no active dinner")
	}

	dinner := channelState.CurrentDinner
	dinner.FinishedAt = time.Now()

	// Initialize the Ratings map if it's nil (just in case)
	if dinner.Ratings == nil {
		s.logger.Info("Initializing Ratings map for dinner %s during FinishDinner", dinner.ID)
		dinner.Ratings = make(map[string]int)
	}

	err = s.store.Set(dinner.ID, dinner)
	if err != nil {
		return err
	}

	// Clear current dinner from channel state
	channelState.CurrentDinner = nil
	channelState.LastActivity = time.Now()

	return s.store.Set(channelKey, channelState)
}

// RateDinner adds a rating to a dinner
func (s *Service) RateDinner(dinnerID, userID string, rating int) error {
	var dinner models.Dinner
	err := s.store.Get(dinnerID, &dinner)
	if err != nil {
		return err
	}

	// Initialize the Ratings map if it's nil
	if dinner.Ratings == nil {
		s.logger.Info("Initializing Ratings map for dinner %s", dinnerID)
		dinner.Ratings = make(map[string]int)
	}

	dinner.Ratings[userID] = rating

	// Calculate average rating
	var sum int
	for _, r := range dinner.Ratings {
		sum += r
	}
	dinner.AverageRating = float64(sum) / float64(len(dinner.Ratings))

	return s.store.Set(dinnerID, dinner)
}

// UpdateUsedIngredients updates the list of ingredients used for a dinner
func (s *Service) UpdateUsedIngredients(dinnerID string, ingredients []string) error {
	var dinner models.Dinner
	err := s.store.Get(dinnerID, &dinner)
	if err != nil {
		return err
	}

	// Initialize the Ratings map if it's nil (just in case)
	if dinner.Ratings == nil {
		s.logger.Info("Initializing Ratings map for dinner %s during UpdateUsedIngredients", dinnerID)
		dinner.Ratings = make(map[string]int)
	}

	dinner.UsedIngredients = ingredients

	return s.store.Set(dinnerID, dinner)
}
