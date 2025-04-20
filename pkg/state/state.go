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
	// StateAddingPhotos is the state when the user is adding photos
	StateAddingPhotos State = "adding_photos"
	// StateSuggestingDish is the state when the user is suggesting a dish
	StateSuggestingDish State = "suggesting_dish"
)

// ChatState represents the state of a chat
type ChatState struct {
	State     State
	Timestamp time.Time
	Data      map[string]string
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

	// Check if the chat already has a state
	if existingState, ok := m.states[chatID]; ok {
		// Update the state but keep the data
		existingState.State = state
		existingState.Timestamp = time.Now()
		m.states[chatID] = existingState
	} else {
		// Create a new state
		m.states[chatID] = ChatState{
			State:     state,
			Timestamp: time.Now(),
			Data:      make(map[string]string),
		}
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

// SetData sets a data value for a chat
func (m *Manager) SetData(chatID int64, key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if the chat already has a state
	if state, ok := m.states[chatID]; ok {
		// Update the data
		if state.Data == nil {
			state.Data = make(map[string]string)
		}
		state.Data[key] = value
		state.Timestamp = time.Now()
		m.states[chatID] = state
	} else {
		// Create a new state with normal state
		state = ChatState{
			State:     StateNormal,
			Timestamp: time.Now(),
			Data:      make(map[string]string),
		}
		state.Data[key] = value
		m.states[chatID] = state
	}
}

// GetData gets a data value for a chat
func (m *Manager) GetData(chatID int64, key string) (string, bool) {
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
			return "", false
		}

		if state.Data != nil {
			if value, ok := state.Data[key]; ok {
				return value, true
			}
		}
	}

	return "", false
}

// ClearData clears a data value for a chat
func (m *Manager) ClearData(chatID int64, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state, ok := m.states[chatID]; ok {
		if state.Data != nil {
			delete(state.Data, key)
			state.Timestamp = time.Now()
			m.states[chatID] = state
		}
	}
}
