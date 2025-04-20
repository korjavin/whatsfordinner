package state

import (
	"sync"
	"time"
)

// State represents the state of a chat
type State string

const (
	// StateNormal is the normal state
	StateNormal State = "normal"
	// StateAddingIngredients is the state when the user is adding ingredients
	StateAddingIngredients State = "adding_ingredients"
)

// ChatState represents the state of a chat
type ChatState struct {
	State     State
	Timestamp time.Time
}

// Manager manages chat states
type Manager struct {
	states map[int64]ChatState
	mu     sync.RWMutex
}

// New creates a new state manager
func New() *Manager {
	return &Manager{
		states: make(map[int64]ChatState),
	}
}

// SetState sets the state for a chat
func (m *Manager) SetState(chatID int64, state State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[chatID] = ChatState{
		State:     state,
		Timestamp: time.Now(),
	}
}

// GetState gets the state for a chat
func (m *Manager) GetState(chatID int64) State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if state, ok := m.states[chatID]; ok {
		// If the state is older than 10 minutes, reset it to normal
		if time.Since(state.Timestamp) > 10*time.Minute {
			m.mu.RUnlock()
			m.mu.Lock()
			delete(m.states, chatID)
			m.mu.Unlock()
			m.mu.RLock()
			return StateNormal
		}
		return state.State
	}
	return StateNormal
}

// ClearState clears the state for a chat
func (m *Manager) ClearState(chatID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, chatID)
}
