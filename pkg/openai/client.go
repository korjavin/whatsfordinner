package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/korjavin/whatsfordinner/pkg/logger"
	"github.com/sashabaranov/go-openai"
)

// Client represents an OpenAI API client
type Client struct {
	client *openai.Client
	model  string
	logger *logger.Logger
}

// New creates a new OpenAI client
func New(apiKey, apiBase, model string) *Client {
	config := openai.DefaultConfig(apiKey)
	if apiBase != "" {
		config.BaseURL = apiBase
	}

	client := openai.NewClientWithConfig(config)
	return &Client{
		client: client,
		model:  model,
		logger: logger.New(""),
	}
}

// GetDishInfo retrieves information about a dish from the LLM
func (c *Client) GetDishInfo(dishName, cuisine string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`
You are a cooking expert. Please provide detailed information about the dish "%s" from %s cuisine.
Return the information in the following JSON format:
{
  "name": "Full dish name",
  "cuisine": "Cuisine type",
  "ingredients": ["ingredient1", "ingredient2", ...],
  "instructions": ["step1", "step2", ...],
  "description": "Brief description of the dish"
}
Only return the JSON, no other text.
`, dishName, cuisine)

	c.logger.Info("Requesting dish info for %s (%s cuisine)", dishName, cuisine)
	c.logger.Debug("OpenAI prompt (first 100 chars): %s", truncateString(prompt, 100))

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.2,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI API")
	}

	content := resp.Choices[0].Message.Content
	c.logger.Debug("OpenAI response (first 100 chars): %s", truncateString(content, 100))

	// Clean up the response - sometimes the model returns markdown code blocks
	content = cleanJSONResponse(content)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		c.logger.Error("Failed to parse response: %v, Content: %s", err, content)
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	return result, nil
}

// GenerateChatMessage generates a chat message for a specific intent
func (c *Client) GenerateChatMessage(intent string, contextData map[string]interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Convert context to JSON string
	contextJSON, err := json.Marshal(contextData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal context: %w", err)
	}

	prompt := fmt.Sprintf(`
You are a friendly cooking assistant bot for a Telegram group. Generate a short, engaging message for the following intent: "%s".
Use the context provided below to personalize the message. Keep it concise and mobile-friendly.
Add appropriate emojis for fun and readability.

Context:
%s

Return only the message text, no explanations or other text.
`, intent, string(contextJSON))

	c.logger.Info("Generating chat message for intent: %s", intent)

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.7,
		},
	)

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI API")
	}

	return resp.Choices[0].Message.Content, nil
}

// ExtractIngredientsFromPhoto extracts ingredients from a photo
func (c *Client) ExtractIngredientsFromPhoto(photoURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`
You are a computer vision expert. Look at the image of a fridge or pantry and list all visible food ingredients.
Return only a JSON array of ingredient names, no other text.
For example: ["eggs", "milk", "tomatoes", "chicken breast"]
`)

	c.logger.Info("Extracting ingredients from photo")

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
				{
					Role: openai.ChatMessageRoleUser,
					MultiContent: []openai.ChatMessagePart{
						{
							Type: openai.ChatMessagePartTypeText,
							Text: "What ingredients do you see in this image?",
						},
						{
							Type: openai.ChatMessagePartTypeImageURL,
							ImageURL: &openai.ChatMessageImageURL{
								URL: photoURL,
							},
						},
					},
				},
			},
			Temperature: 0.2,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI API")
	}

	content := resp.Choices[0].Message.Content
	var ingredients []string

	if err := json.Unmarshal([]byte(content), &ingredients); err != nil {
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	return ingredients, nil
}

// ParseIngredientsFromText extracts ingredients from free-form text
func (c *Client) ParseIngredientsFromText(text string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`
You are a cooking assistant. Extract all food ingredients from the following text.
Return only a JSON array of ingredient names, no other text.
For example: ["eggs", "milk", "tomatoes", "chicken breast"]

Text: %s
`, text)

	c.logger.Info("Parsing ingredients from text")
	c.logger.Debug("Text to parse (first 100 chars): %s", truncateString(text, 100))

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.2,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI API")
	}

	content := resp.Choices[0].Message.Content
	c.logger.Debug("OpenAI response (first 100 chars): %s", truncateString(content, 100))

	// Clean up the response - sometimes the model returns markdown code blocks
	content = cleanJSONResponse(content)

	var ingredients []string
	if err := json.Unmarshal([]byte(content), &ingredients); err != nil {
		c.logger.Error("Failed to parse response: %v, Content: %s", err, content)
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	return ingredients, nil
}

// Helper functions

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// cleanJSONResponse cleans up the JSON response from OpenAI
// Sometimes the model returns markdown code blocks with ```json and ``` delimiters
func cleanJSONResponse(s string) string {
	// Remove markdown code block delimiters if present
	s = strings.TrimSpace(s)

	// Check for markdown code blocks
	if strings.HasPrefix(s, "```") {
		// Find the end of the first line (which might contain "```json")
		firstLineEnd := strings.Index(s, "\n")
		if firstLineEnd != -1 {
			// Skip the first line
			s = s[firstLineEnd+1:]
		}

		// Remove trailing code block delimiter
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}

		// Trim any remaining whitespace
		s = strings.TrimSpace(s)
	}

	return s
}
