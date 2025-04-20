package poll

import (
	"fmt"
	"math"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/storage"
)

// Service provides poll management functionality
type Service struct {
	store  *storage.Store
	logger *logger.Logger
}

// New creates a new poll service
func New(store *storage.Store) *Service {
	return &Service{
		store:  store,
		logger: logger.New(""),
	}
}

// CreateVote creates a new vote
func (s *Service) CreateVote(channelID int64, pollID string, messageID int, options []string) (*models.VoteState, error) {
	vote := &models.VoteState{
		PollID:    pollID,
		MessageID: messageID,
		Options:   options,
		Votes:     make(map[string]string),
		StartedAt: time.Now(),
	}

	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	err := s.store.Set(voteKey, vote)
	if err != nil {
		return nil, err
	}

	// Create a direct mapping from poll ID to channel ID for easier lookup
	pollMappingKey := fmt.Sprintf("poll_mapping:%s", pollID)
	err = s.store.Set(pollMappingKey, channelID)
	if err != nil {
		s.logger.Error("Failed to create poll mapping: %v", err)
		// Continue anyway, as this is just an optimization
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

	channelState.CurrentVote = vote
	channelState.LastActivity = time.Now()

	err = s.store.Set(channelKey, channelState)
	if err != nil {
		return nil, err
	}

	return vote, nil
}

// RecordVote records a vote from a user
func (s *Service) RecordVote(channelID int64, pollID, userID, option string) error {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return err
	}

	// Check if the option is valid
	optionValid := false
	for _, validOption := range vote.Options {
		if option == validOption {
			optionValid = true
			break
		}
	}

	if !optionValid {
		return fmt.Errorf("invalid option: %s", option)
	}

	// Record the vote
	vote.Votes[userID] = option

	return s.store.Set(voteKey, vote)
}

// GetVoteResults returns the results of a vote
func (s *Service) GetVoteResults(channelID int64, pollID string) (map[string]int, string, error) {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return nil, "", err
	}

	// Count votes for each option
	results := make(map[string]int)
	for _, option := range vote.Options {
		results[option] = 0
	}

	for _, option := range vote.Votes {
		results[option]++
	}

	// Find the winning option
	var winningOption string
	var maxVotes int
	for option, count := range results {
		if count > maxVotes {
			maxVotes = count
			winningOption = option
		}
	}

	return results, winningOption, nil
}

// EndVote marks a vote as ended and records the winning dish
func (s *Service) EndVote(channelID int64, pollID, winningDish string) error {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return err
	}

	vote.EndedAt = time.Now()
	vote.WinningDish = winningDish

	err = s.store.Set(voteKey, vote)
	if err != nil {
		return err
	}

	// Update channel state
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err = s.store.Get(channelKey, &channelState)
	if err != nil {
		return err
	}

	// Only clear current vote if it's the same as the one we're ending
	if channelState.CurrentVote != nil && channelState.CurrentVote.PollID == pollID {
		channelState.CurrentVote = nil
		channelState.LastActivity = time.Now()
		err = s.store.Set(channelKey, channelState)
		if err != nil {
			return err
		}
	}

	return nil
}

// AddCookVolunteer adds a cook volunteer to a vote
func (s *Service) AddCookVolunteer(channelID int64, pollID, userID string) error {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return err
	}

	// Check if the user voted for the winning dish
	if vote.Votes[userID] != vote.WinningDish && len(vote.Votes) > 0 {
		return fmt.Errorf("user did not vote for the winning dish")
	}

	// Add the volunteer if not already added
	for _, volunteer := range vote.CookVolunteers {
		if volunteer == userID {
			return nil // Already volunteered
		}
	}

	vote.CookVolunteers = append(vote.CookVolunteers, userID)

	return s.store.Set(voteKey, vote)
}

// SelectCook selects a cook from the volunteers
func (s *Service) SelectCook(channelID int64, pollID, userID string) error {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return err
	}

	// Check if the user is a volunteer
	isVolunteer := false
	for _, volunteer := range vote.CookVolunteers {
		if volunteer == userID {
			isVolunteer = true
			break
		}
	}

	if !isVolunteer {
		return fmt.Errorf("user is not a volunteer")
	}

	vote.SelectedCook = userID

	return s.store.Set(voteKey, vote)
}

// CheckVoteThreshold checks if the vote has reached the threshold to be closed
// Returns true if the threshold is reached, the winning option, and an error if any
func (s *Service) CheckVoteThreshold(channelID int64, pollID string, channelMemberCount int, thresholdPercent float64) (bool, string, error) {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return false, "", err
	}

	// If the vote is already ended, return false
	if !vote.EndedAt.IsZero() {
		return false, "", nil
	}

	// Calculate the threshold
	threshold := int(math.Ceil(float64(channelMemberCount) * thresholdPercent))
	s.logger.Debug("Threshold: %d (channel members: %d, threshold percent: %.2f)", threshold, channelMemberCount, thresholdPercent)

	// Count the total votes
	totalVotes := len(vote.Votes)
	s.logger.Debug("Total votes: %d", totalVotes)

	// Check if the threshold is reached
	if totalVotes >= threshold {
		// Get the results
		_, winningOption, err := s.GetVoteResults(channelID, pollID)
		if err != nil {
			return false, "", err
		}

		return true, winningOption, nil
	}

	return false, "", nil
}

// GetVote gets a vote by its ID
func (s *Service) GetVote(channelID int64, pollID string) (*models.VoteState, error) {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return nil, err
	}

	return &vote, nil
}

// FindChannelByPollID finds the channel ID that contains a poll with the given ID
func (s *Service) FindChannelByPollID(pollID string) (int64, error) {
	// Create a direct mapping for poll ID to channel ID
	pollMappingKey := fmt.Sprintf("poll_mapping:%s", pollID)
	var channelID int64
	err := s.store.Get(pollMappingKey, &channelID)
	if err == nil {
		// We found a direct mapping
		return channelID, nil
	}

	// If we don't have a direct mapping, search through all channels
	channelKeys, err := s.store.List("channel:")
	if err != nil {
		return 0, fmt.Errorf("failed to list channels: %w", err)
	}

	for _, channelKey := range channelKeys {
		var channelState models.ChannelState
		err := s.store.Get(channelKey, &channelState)
		if err != nil {
			s.logger.Error("Failed to get channel state: %v", err)
			continue
		}

		// Check if this channel has a current vote with this poll ID
		if channelState.CurrentVote != nil && channelState.CurrentVote.PollID == pollID {
			return channelState.ChannelID, nil
		}

		// Also check for past votes
		voteKeys, err := s.store.List(fmt.Sprintf("vote:%d:", channelState.ChannelID))
		if err != nil {
			s.logger.Error("Failed to list votes for channel %d: %v", channelState.ChannelID, err)
			continue
		}

		for _, voteKey := range voteKeys {
			var vote models.VoteState
			err := s.store.Get(voteKey, &vote)
			if err != nil {
				s.logger.Error("Failed to get vote %s: %v", voteKey, err)
				continue
			}

			if vote.PollID == pollID {
				// Create a mapping for future lookups
				s.store.Set(pollMappingKey, channelState.ChannelID)
				return channelState.ChannelID, nil
			}
		}
	}

	return 0, fmt.Errorf("channel not found for poll %s", pollID)
}

// GetCurrentVote gets the current vote for a channel
func (s *Service) GetCurrentVote(channelID int64) (*models.VoteState, error) {
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err := s.store.Get(channelKey, &channelState)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel state: %w", err)
	}

	if channelState.CurrentVote == nil {
		return nil, fmt.Errorf("no current vote for channel %d", channelID)
	}

	return channelState.CurrentVote, nil
}

// AddOptionToVote adds a new option to an existing vote and returns the updated vote
// Note: This doesn't update the actual Telegram poll - that needs to be done separately
func (s *Service) AddOptionToVote(channelID int64, pollID string, newOption string) (*models.VoteState, error) {
	voteKey := fmt.Sprintf("vote:%d:%s", channelID, pollID)
	var vote models.VoteState
	err := s.store.Get(voteKey, &vote)
	if err != nil {
		return nil, fmt.Errorf("failed to get vote: %w", err)
	}

	// Check if the vote has already ended
	if !vote.EndedAt.IsZero() {
		return nil, fmt.Errorf("vote has already ended")
	}

	// Check if the option already exists
	for _, option := range vote.Options {
		if option == newOption {
			return nil, fmt.Errorf("option already exists: %s", newOption)
		}
	}

	// Add the new option
	vote.Options = append(vote.Options, newOption)

	// Save the updated vote
	err = s.store.Set(voteKey, vote)
	if err != nil {
		return nil, fmt.Errorf("failed to save updated vote: %w", err)
	}

	return &vote, nil
}
