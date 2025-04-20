package poll

import (
	"fmt"
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
