package stats

import (
	"fmt"
	"sort"

	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/storage"
)

// Service provides statistics functionality
type Service struct {
	store  *storage.Store
	logger *logger.Logger
}

// New creates a new statistics service
func New(store *storage.Store) *Service {
	return &Service{
		store:  store,
		logger: logger.New(""),
	}
}

// GetStatistics retrieves the statistics for a channel
func (s *Service) GetStatistics(channelID int64) (*models.Statistics, error) {
	statsKey := fmt.Sprintf("stats:%d", channelID)

	var stats models.Statistics
	err := s.store.Get(statsKey, &stats)
	if err != nil {
		// If the statistics don't exist, create new ones
		stats = models.Statistics{
			ChannelID:      channelID,
			CookStats:      make(map[string]models.CookStat),
			HelperStats:    make(map[string]models.HelperStat),
			SuggesterStats: make(map[string]models.SuggesterStat),
		}

		if err := s.store.Set(statsKey, stats); err != nil {
			return nil, fmt.Errorf("failed to create statistics: %w", err)
		}
	}

	return &stats, nil
}

// UpdateCookStats updates the cook statistics for a user
func (s *Service) UpdateCookStats(channelID int64, userID, username string, rating float64) error {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return err
	}

	// Get or create cook stats for the user
	cookStat, exists := stats.CookStats[userID]
	if !exists {
		cookStat = models.CookStat{
			UserID:   userID,
			Username: username,
		}
	} else if username != "" && cookStat.Username == "" {
		// Update username if it's empty and we have a new one
		cookStat.Username = username
	}

	// Update cook stats
	cookStat.CookCount++
	cookStat.TotalRating += rating
	cookStat.AvgRating = cookStat.TotalRating / float64(cookStat.CookCount)

	// Save updated stats
	stats.CookStats[userID] = cookStat

	return s.store.Set(fmt.Sprintf("stats:%d", channelID), stats)
}

// UpdateHelperStats updates the helper statistics for a user
func (s *Service) UpdateHelperStats(channelID int64, userID, username string) error {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return err
	}

	// Get or create helper stats for the user
	helperStat, exists := stats.HelperStats[userID]
	if !exists {
		helperStat = models.HelperStat{
			UserID:   userID,
			Username: username,
		}
	} else if username != "" && helperStat.Username == "" {
		// Update username if it's empty and we have a new one
		helperStat.Username = username
	}

	// Update helper stats
	helperStat.ShoppingCount++

	// Save updated stats
	stats.HelperStats[userID] = helperStat

	return s.store.Set(fmt.Sprintf("stats:%d", channelID), stats)
}

// UpdateSuggesterStats updates the suggester statistics for a user
func (s *Service) UpdateSuggesterStats(channelID int64, userID, username string, accepted bool) error {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return err
	}

	// Get or create suggester stats for the user
	suggesterStat, exists := stats.SuggesterStats[userID]
	if !exists {
		suggesterStat = models.SuggesterStat{
			UserID:   userID,
			Username: username,
		}
	} else if username != "" && suggesterStat.Username == "" {
		// Update username if it's empty and we have a new one
		suggesterStat.Username = username
	}

	// Update suggester stats
	suggesterStat.SuggestionCount++
	if accepted {
		suggesterStat.AcceptedCount++
	}

	// Save updated stats
	stats.SuggesterStats[userID] = suggesterStat

	return s.store.Set(fmt.Sprintf("stats:%d", channelID), stats)
}

// GetTopCooks returns the top cooks by average rating
func (s *Service) GetTopCooks(channelID int64, limit int) ([]models.CookStat, error) {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return nil, err
	}

	// Convert map to slice for sorting
	cooks := make([]models.CookStat, 0, len(stats.CookStats))
	for _, cookStat := range stats.CookStats {
		cooks = append(cooks, cookStat)
	}

	// Sort by average rating (descending)
	sort.Slice(cooks, func(i, j int) bool {
		return cooks[i].AvgRating > cooks[j].AvgRating
	})

	// Take the top N cooks
	if len(cooks) > limit {
		cooks = cooks[:limit]
	}

	return cooks, nil
}

// GetTopHelpers returns the top helpers by shopping count
func (s *Service) GetTopHelpers(channelID int64, limit int) ([]models.HelperStat, error) {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return nil, err
	}

	// Convert map to slice for sorting
	helpers := make([]models.HelperStat, 0, len(stats.HelperStats))
	for _, helperStat := range stats.HelperStats {
		helpers = append(helpers, helperStat)
	}

	// Sort by shopping count (descending)
	sort.Slice(helpers, func(i, j int) bool {
		return helpers[i].ShoppingCount > helpers[j].ShoppingCount
	})

	// Take the top N helpers
	if len(helpers) > limit {
		helpers = helpers[:limit]
	}

	return helpers, nil
}

// GetTopSuggesters returns the top suggesters by acceptance rate
func (s *Service) GetTopSuggesters(channelID int64, limit int) ([]models.SuggesterStat, error) {
	stats, err := s.GetStatistics(channelID)
	if err != nil {
		return nil, err
	}

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

	// Take the top N suggesters
	if len(suggesters) > limit {
		suggesters = suggesters[:limit]
	}

	return suggesters, nil
}
