package models

import (
	"time"
)

// ChannelState represents the state of a Telegram channel
type ChannelState struct {
	ChannelID     int64      `json:"channel_id"`
	FridgeID      string     `json:"fridge_id"`
	CurrentDinner *Dinner    `json:"current_dinner,omitempty"`
	CurrentVote   *VoteState `json:"current_vote,omitempty"`
	LastActivity  time.Time  `json:"last_activity"`
	Cuisines      []string   `json:"cuisines"`
	MemberCount   int        `json:"member_count,omitempty"`
}

// Fridge represents the ingredients available in a channel's fridge
type Fridge struct {
	ID          string                `json:"id"`
	ChannelID   int64                 `json:"channel_id"`
	Ingredients map[string]Ingredient `json:"ingredients"`
	LastUpdated time.Time             `json:"last_updated"`
}

// Ingredient represents a single ingredient in the fridge
type Ingredient struct {
	Name     string    `json:"name"`
	Quantity string    `json:"quantity,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// Dish represents a dinner dish
type Dish struct {
	Name         string   `json:"name"`
	Cuisine      string   `json:"cuisine"`
	Ingredients  []string `json:"ingredients"`
	Instructions []string `json:"instructions"`
}

// VoteState represents the state of a vote
type VoteState struct {
	PollID         string            `json:"poll_id"`
	MessageID      int               `json:"message_id"`
	Options        []string          `json:"options"`
	Votes          map[string]string `json:"votes"` // UserID -> Option
	StartedAt      time.Time         `json:"started_at"`
	EndedAt        time.Time         `json:"ended_at,omitempty"`
	WinningDish    string            `json:"winning_dish,omitempty"`
	CookVolunteers []string          `json:"cook_volunteers,omitempty"`
	SelectedCook   string            `json:"selected_cook,omitempty"`
}

// Dinner represents a dinner event
type Dinner struct {
	ID              string         `json:"id"`
	ChannelID       int64          `json:"channel_id"`
	Dish            Dish           `json:"dish"`
	Cook            string         `json:"cook,omitempty"` // UserID of the cook
	StartedAt       time.Time      `json:"started_at"`
	FinishedAt      time.Time      `json:"finished_at,omitempty"`
	Ratings         map[string]int `json:"ratings,omitempty"` // UserID -> Rating (1-5)
	AverageRating   float64        `json:"average_rating,omitempty"`
	UsedIngredients []string       `json:"used_ingredients,omitempty"`
}

// Statistics represents the statistics for a channel
type Statistics struct {
	ChannelID      int64                    `json:"channel_id"`
	CookStats      map[string]CookStat      `json:"cook_stats"`      // UserID -> CookStat
	HelperStats    map[string]HelperStat    `json:"helper_stats"`    // UserID -> HelperStat
	SuggesterStats map[string]SuggesterStat `json:"suggester_stats"` // UserID -> SuggesterStat
}

// CookStat represents the statistics for a cook
type CookStat struct {
	UserID      string  `json:"user_id"`
	Username    string  `json:"username"`
	CookCount   int     `json:"cook_count"`
	TotalRating float64 `json:"total_rating"`
	AvgRating   float64 `json:"avg_rating"`
}

// HelperStat represents the statistics for a shopping helper
type HelperStat struct {
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
	ShoppingCount int    `json:"shopping_count"`
}

// SuggesterStat represents the statistics for a dish suggester
type SuggesterStat struct {
	UserID          string `json:"user_id"`
	Username        string `json:"username"`
	SuggestionCount int    `json:"suggestion_count"`
	AcceptedCount   int    `json:"accepted_count"`
}

// SuggestedDish represents a dish suggested by a user
type SuggestedDish struct {
	ID          string    `json:"id"`
	ChannelID   int64     `json:"channel_id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	Cuisine     string    `json:"cuisine"`
	Description string    `json:"description"`
	SuggestedAt time.Time `json:"suggested_at"`
	UsedInPoll  bool      `json:"used_in_poll"`
}
