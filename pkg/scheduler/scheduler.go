package scheduler

import (
	"fmt"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/dinner"
	"github.com/korjavin/whatsfordinner/pkg/fridge"
	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/korjavin/whatsfordinner/pkg/models"
	"github.com/korjavin/whatsfordinner/pkg/openai"
	"github.com/korjavin/whatsfordinner/pkg/poll"
	"github.com/korjavin/whatsfordinner/pkg/storage"
	"github.com/korjavin/whatsfordinner/pkg/telegram"
)

// Service provides scheduling functionality for dinner workflows
type Service struct {
	store         *storage.Store
	bot           *telegram.Bot
	fridgeService *fridge.Service
	pollService   *poll.Service
	dinnerService *dinner.Service
	openaiClient  *openai.Client
	logger        *logger.Logger
	cuisines      []string
	stopChan      chan struct{}
}

// New creates a new scheduler service
func New(
	store *storage.Store,
	bot *telegram.Bot,
	fridgeService *fridge.Service,
	pollService *poll.Service,
	dinnerService *dinner.Service,
	openaiClient *openai.Client,
	cuisines []string,
) *Service {
	return &Service{
		store:         store,
		bot:           bot,
		fridgeService: fridgeService,
		pollService:   pollService,
		dinnerService: dinnerService,
		openaiClient:  openaiClient,
		logger:        logger.New("scheduler"),
		cuisines:      cuisines,
		stopChan:      make(chan struct{}),
	}
}

// Start starts the scheduler
func (s *Service) Start() {
	s.logger.Info("Starting dinner scheduler")
	
	// Start the daily dinner scheduler
	go s.runDailyDinnerScheduler()
	
	// Start the dinner timeout checker
	go s.runDinnerTimeoutChecker()
	
	// Start the cook volunteer timeout checker
	go s.runCookVolunteerTimeoutChecker()
}

// Stop stops the scheduler
func (s *Service) Stop() {
	s.logger.Info("Stopping dinner scheduler")
	close(s.stopChan)
}

// runDailyDinnerScheduler runs the daily dinner scheduler
// It starts the dinner workflow at 3pm if it hasn't been started today
func (s *Service) runDailyDinnerScheduler() {
	s.logger.Info("Starting daily dinner scheduler")
	
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			
			// Check if it's around 3pm (15:00)
			if now.Hour() == 15 && now.Minute() < 5 {
				s.logger.Info("It's 3pm, checking if dinner workflow needs to be started")
				
				// Get all channels
				channelKeys, err := s.store.List("channel:")
				if err != nil {
					s.logger.Error("Failed to list channels: %v", err)
					continue
				}
				
				for _, channelKey := range channelKeys {
					var channelState models.ChannelState
					err := s.store.Get(channelKey, &channelState)
					if err != nil {
						s.logger.Error("Failed to get channel state: %v", err)
						continue
					}
					
					// Check if dinner workflow has been started today
					if !s.hasDinnerStartedToday(channelState) {
						s.logger.Info("Starting dinner workflow for channel %d", channelState.ChannelID)
						s.startDinnerWorkflow(channelState.ChannelID)
					}
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

// runDinnerTimeoutChecker checks for dinner workflows that need to be stopped at 9pm
func (s *Service) runDinnerTimeoutChecker() {
	s.logger.Info("Starting dinner timeout checker")
	
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			
			// Check if it's around 9pm (21:00)
			if now.Hour() == 21 && now.Minute() < 5 {
				s.logger.Info("It's 9pm, checking for unfinished dinner workflows")
				
				// Get all channels
				channelKeys, err := s.store.List("channel:")
				if err != nil {
					s.logger.Error("Failed to list channels: %v", err)
					continue
				}
				
				for _, channelKey := range channelKeys {
					var channelState models.ChannelState
					err := s.store.Get(channelKey, &channelState)
					if err != nil {
						s.logger.Error("Failed to get channel state: %v", err)
						continue
					}
					
					// Check if there's an active dinner or vote
					if s.hasUnfinishedDinnerWorkflow(channelState) {
						s.logger.Info("Stopping unfinished dinner workflow for channel %d", channelState.ChannelID)
						s.stopDinnerWorkflow(channelState.ChannelID)
					}
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

// runCookVolunteerTimeoutChecker checks for votes that need a cook volunteer
func (s *Service) runCookVolunteerTimeoutChecker() {
	s.logger.Info("Starting cook volunteer timeout checker")
	
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	// Map to track when we started waiting for volunteers
	volunteerWaitStart := make(map[string]time.Time)
	
	for {
		select {
		case <-ticker.C:
			// Get all channels
			channelKeys, err := s.store.List("channel:")
			if err != nil {
				s.logger.Error("Failed to list channels: %v", err)
				continue
			}
			
			for _, channelKey := range channelKeys {
				var channelState models.ChannelState
				err := s.store.Get(channelKey, &channelState)
				if err != nil {
					s.logger.Error("Failed to get channel state: %v", err)
					continue
				}
				
				// Check if there's a vote that has ended but no cook has been selected
				if channelState.CurrentVote != nil && 
				   !channelState.CurrentVote.EndedAt.IsZero() && 
				   channelState.CurrentVote.WinningDish != "" && 
				   len(channelState.CurrentVote.CookVolunteers) == 0 {
					
					voteID := channelState.CurrentVote.PollID
					
					// Check if we're already tracking this vote
					startTime, exists := volunteerWaitStart[voteID]
					if !exists {
						// Start tracking this vote
						volunteerWaitStart[voteID] = time.Now()
						s.logger.Info("Started waiting for cook volunteers for vote %s in channel %d", voteID, channelState.ChannelID)
					} else {
						// Check if 15 minutes have passed
						if time.Since(startTime) > 15*time.Minute {
							s.logger.Info("No cook volunteers after 15 minutes for vote %s in channel %d", voteID, channelState.ChannelID)
							
							// Remove from tracking
							delete(volunteerWaitStart, voteID)
							
							// Restart the dinner workflow
							s.restartDinnerWorkflow(channelState.ChannelID)
						}
					}
				} else if channelState.CurrentVote != nil && len(channelState.CurrentVote.CookVolunteers) > 0 {
					// If there are volunteers, remove from tracking
					voteID := channelState.CurrentVote.PollID
					if _, exists := volunteerWaitStart[voteID]; exists {
						delete(volunteerWaitStart, voteID)
					}
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

// hasDinnerStartedToday checks if a dinner workflow has been started today for a channel
func (s *Service) hasDinnerStartedToday(channelState models.ChannelState) bool {
	// Check if there's a current dinner or vote
	if channelState.CurrentDinner != nil || channelState.CurrentVote != nil {
		return true
	}
	
	// Check for any dinner that started today
	today := time.Now().Truncate(24 * time.Hour)
	dinnerKeys, err := s.store.List(fmt.Sprintf("dinner:%d:", channelState.ChannelID))
	if err != nil {
		s.logger.Error("Failed to list dinners: %v", err)
		return false
	}
	
	for _, dinnerKey := range dinnerKeys {
		var dinner models.Dinner
		err := s.store.Get(dinnerKey, &dinner)
		if err != nil {
			s.logger.Error("Failed to get dinner %s: %v", dinnerKey, err)
			continue
		}
		
		// Check if the dinner started today
		if dinner.StartedAt.After(today) {
			return true
		}
	}
	
	// Check for any vote that started today
	voteKeys, err := s.store.List(fmt.Sprintf("vote:%d:", channelState.ChannelID))
	if err != nil {
		s.logger.Error("Failed to list votes: %v", err)
		return false
	}
	
	for _, voteKey := range voteKeys {
		var vote models.VoteState
		err := s.store.Get(voteKey, &vote)
		if err != nil {
			s.logger.Error("Failed to get vote %s: %v", voteKey, err)
			continue
		}
		
		// Check if the vote started today
		if vote.StartedAt.After(today) {
			return true
		}
	}
	
	return false
}

// hasUnfinishedDinnerWorkflow checks if there's an unfinished dinner workflow
func (s *Service) hasUnfinishedDinnerWorkflow(channelState models.ChannelState) bool {
	// Check if there's a current dinner that hasn't finished
	if channelState.CurrentDinner != nil && channelState.CurrentDinner.FinishedAt.IsZero() {
		return true
	}
	
	// Check if there's a current vote that hasn't ended
	if channelState.CurrentVote != nil && channelState.CurrentVote.EndedAt.IsZero() {
		return true
	}
	
	return false
}

// startDinnerWorkflow starts the dinner workflow for a channel
func (s *Service) startDinnerWorkflow(channelID int64) {
	s.logger.Info("Starting dinner workflow for channel %d", channelID)
	
	// Send a message to the channel
	s.bot.SendMessage(channelID, "🕒 It's dinner time! Let me suggest some options based on your fridge...")
	
	// Get ingredients from the fridge
	ingredients, err := s.fridgeService.ListIngredients(channelID)
	if err != nil {
		s.logger.Error("Failed to list ingredients: %v", err)
		errorMsg := "😢 Sorry, I couldn't retrieve your fridge contents. Please try again later or use the /dinner command manually."
		s.bot.SendMessage(channelID, errorMsg)
		return
	}
	
	if len(ingredients) == 0 {
		s.bot.SendMessage(channelID, "😢 Your fridge is empty! Please add some ingredients with /sync_fridge or /add_photo before I can suggest dinner options.")
		return
	}
	
	// Extract ingredient names
	ingredientNames := make([]string, len(ingredients))
	for i, ingredient := range ingredients {
		ingredientNames[i] = ingredient.Name
	}
	
	// Send a processing message
	processingMsg, _ := s.bot.SendMessage(channelID, "🧐 Thinking about dinner options based on your ingredients... This might take a moment.")
	
	// Get dinner suggestions from OpenAI
	aiSuggestions, err := s.openaiClient.SuggestDinnerOptions(ingredientNames, s.cuisines, 4)
	if err != nil {
		s.logger.Error("Failed to get dinner suggestions: %v", err)
		s.bot.EditMessage(channelID, processingMsg.MessageID, "😢 Sorry, I couldn't come up with dinner suggestions right now. Please try again later or use the /dinner command manually.")
		return
	}
	
	if len(aiSuggestions) == 0 {
		s.bot.EditMessage(channelID, processingMsg.MessageID, "😢 I couldn't find any suitable dishes based on your fridge contents. Try adding more ingredients with /fridge or suggest your own dishes with /suggest.")
		return
	}
	
	// Create options for the poll
	options := make([]string, len(aiSuggestions))
	
	// Create a detailed message with suggestions
	detailedMsg := "🍲 Here are some dinner suggestions based on your ingredients:\n\n"
	
	// Add AI suggestions
	for i, suggestion := range aiSuggestions {
		name, _ := suggestion["name"].(string)
		cuisine, _ := suggestion["cuisine"].(string)
		description, _ := suggestion["description"].(string)
		
		options[i] = name
		
		detailedMsg += fmt.Sprintf("🍴 *%s* (%s)\n%s\n\n", name, cuisine, description)
	}
	
	// Edit the processing message to show the detailed suggestions
	s.bot.EditMessage(channelID, processingMsg.MessageID, detailedMsg)
	
	// Create poll
	pollMsg, err := s.bot.CreatePoll(channelID, "What should we cook tonight?", options)
	if err != nil {
		s.logger.Error("Failed to create poll: %v", err)
		s.bot.SendMessage(channelID, "😢 Sorry, I couldn't create a poll for dinner options. Please try again later or use the /dinner command manually.")
		return
	}
	
	// Store vote state
	pollID := pollMsg.Poll.ID
	s.logger.Info("Created poll with ID %s for channel %d", pollID, channelID)
	
	_, err = s.pollService.CreateVote(channelID, pollID, pollMsg.MessageID, options)
	if err != nil {
		s.logger.Error("Failed to create vote state: %v", err)
	}
	
	// Send a message with voting instructions
	s.bot.SendMessage(channelID, "🗳 Please vote for your preferred dinner option! The poll will close automatically when 2/3 of the channel members have voted.")
}

// stopDinnerWorkflow stops the dinner workflow for a channel
func (s *Service) stopDinnerWorkflow(channelID int64) {
	s.logger.Info("Stopping dinner workflow for channel %d", channelID)
	
	// Get channel state
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err := s.store.Get(channelKey, &channelState)
	if err != nil {
		s.logger.Error("Failed to get channel state: %v", err)
		return
	}
	
	// Check if there's an active vote
	if channelState.CurrentVote != nil && channelState.CurrentVote.EndedAt.IsZero() {
		// End the vote
		s.logger.Info("Ending vote %s for channel %d", channelState.CurrentVote.PollID, channelID)
		
		// Get the current results
		results, winningOption, err := s.pollService.GetVoteResults(channelID, channelState.CurrentVote.PollID)
		if err != nil {
			s.logger.Error("Failed to get vote results: %v", err)
			winningOption = "No winner"
		}
		
		// End the vote
		err = s.pollService.EndVote(channelID, channelState.CurrentVote.PollID, winningOption)
		if err != nil {
			s.logger.Error("Failed to end vote: %v", err)
		}
		
		// Send a message
		s.bot.SendMessage(channelID, "⏰ It's getting late! The dinner poll has been closed automatically.")
		
		// If there are votes, announce the winner
		if len(results) > 0 {
			s.bot.SendMessage(channelID, fmt.Sprintf("🏆 The winning dish is *%s*.", winningOption))
		} else {
			s.bot.SendMessage(channelID, "😢 Nobody voted for dinner today.")
		}
	}
	
	// Check if there's an active dinner
	if channelState.CurrentDinner != nil && channelState.CurrentDinner.FinishedAt.IsZero() {
		// Finish the dinner
		s.logger.Info("Finishing dinner %s for channel %d", channelState.CurrentDinner.ID, channelID)
		
		err := s.dinnerService.FinishDinner(channelID)
		if err != nil {
			s.logger.Error("Failed to finish dinner: %v", err)
		}
		
		// Send a message
		s.bot.SendMessage(channelID, "⏰ It's getting late! The dinner has been marked as finished automatically.")
	}
}

// restartDinnerWorkflow restarts the dinner workflow for a channel
func (s *Service) restartDinnerWorkflow(channelID int64) {
	s.logger.Info("Restarting dinner workflow for channel %d", channelID)
	
	// Get channel state
	channelKey := fmt.Sprintf("channel:%d", channelID)
	var channelState models.ChannelState
	err := s.store.Get(channelKey, &channelState)
	if err != nil {
		s.logger.Error("Failed to get channel state: %v", err)
		return
	}
	
	// Check if there's an active vote that has ended
	if channelState.CurrentVote != nil && !channelState.CurrentVote.EndedAt.IsZero() {
		// Send a message
		s.bot.SendMessage(channelID, "⏰ 15 minutes have passed and nobody volunteered to cook. Let's try again with a new poll!")
		
		// Clear the current vote
		channelState.CurrentVote = nil
		err = s.store.Set(channelKey, channelState)
		if err != nil {
			s.logger.Error("Failed to update channel state: %v", err)
		}
		
		// Start a new dinner workflow
		s.startDinnerWorkflow(channelID)
	}
}
