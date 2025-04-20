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
func (c *Client) GetDishInfo(dishName string, cuisine ...string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var prompt string
	if len(cuisine) > 0 && cuisine[0] != "" {
		// If cuisine is provided, use it
		prompt = fmt.Sprintf(`
You are a cooking expert. Please provide detailed information about the dish "%s" from %s cuisine.
Return the information in the following JSON format:
{
  "name": "Full dish name",
  "cuisine": "Cuisine type",
  "ingredients_needed": ["ingredient1", "ingredient2", ...],
  "instructions": ["step1", "step2", ...],
  "description": "Brief description of the dish"
}
Only return the JSON, no other text.
`, dishName, cuisine[0])
		c.logger.Info("Requesting dish info for %s (%s cuisine)", dishName, cuisine[0])
	} else {
		// If no cuisine is provided, let the model determine it
		prompt = fmt.Sprintf(`
You are a cooking expert. Please provide detailed information about the dish "%s".
Determine the most likely cuisine for this dish.
Return the information in the following JSON format:
{
  "name": "Full dish name",
  "cuisine": "Cuisine type",
  "ingredients_needed": ["ingredient1", "ingredient2", ...],
  "instructions": ["step1", "step2", ...],
  "description": "Brief description of the dish"
}
Only return the JSON, no other text.
`, dishName)
		c.logger.Info("Requesting dish info for %s (cuisine not specified)", dishName)
	}

	c.logger.Debug("OpenAI prompt (first 100 chars): %s", truncateString(prompt, 100))

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a cooking expert who provides accurate information about dishes and recipes.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.3,
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

	c.logger.Info("Successfully got information for dish: %s", dishName)
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

	prompt := `You are a computer vision expert. Look at the image of a fridge or pantry and list all visible food ingredients.
Be thorough and try to identify as many food items as possible.
Return only a JSON array of ingredient names, no other text.
For example: ["eggs", "milk", "tomatoes", "chicken breast"]
`

	c.logger.Info("Extracting ingredients from photo")
	c.logger.Debug("Photo URL (truncated): %s", truncateString(photoURL, 50))

	// Create a request with the image
	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: prompt,
				},
				{
					Role: openai.ChatMessageRoleUser,
					MultiContent: []openai.ChatMessagePart{
						{
							Type: openai.ChatMessagePartTypeText,
							Text: "What food ingredients do you see in this image? List all of them in a JSON array.",
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
		c.logger.Error("OpenAI API error: %v", err)
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		c.logger.Error("No response from OpenAI API")
		return nil, fmt.Errorf("no response from OpenAI API")
	}

	content := resp.Choices[0].Message.Content
	c.logger.Debug("OpenAI response (first 100 chars): %s", truncateString(content, 100))

	// Clean up the response - sometimes the model returns markdown code blocks
	content = cleanJSONResponse(content)

	var ingredients []string
	if err := json.Unmarshal([]byte(content), &ingredients); err != nil {
		c.logger.Error("Failed to parse response: %v, Content: %s", err, content)

		// Try to extract ingredients using a more lenient approach
		extractedIngredients := extractIngredientsFromText(content)
		if len(extractedIngredients) > 0 {
			c.logger.Info("Extracted %d ingredients using fallback method", len(extractedIngredients))
			return extractedIngredients, nil
		}

		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	c.logger.Info("Successfully extracted %d ingredients from photo", len(ingredients))
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

// SuggestDinnerOptions suggests dinner options based on available ingredients and cuisines
func (c *Client) SuggestDinnerOptions(ingredients []string, cuisines []string, count int) ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert ingredients and cuisines to strings for the prompt
	ingredientsStr := strings.Join(ingredients, ", ")
	cuisinesStr := strings.Join(cuisines, ", ")

	prompt := fmt.Sprintf(`
You are a cooking expert. Based on the available ingredients and preferred cuisines, suggest %d dinner options.

Available ingredients: %s

Preferred cuisines: %s

Return the suggestions in the following JSON format:
[
  {
    "name": "Dish name",
    "cuisine": "Cuisine type",
    "description": "Brief description of the dish",
    "ingredients_needed": ["ingredient1", "ingredient2", ...],
    "ingredients_missing": ["ingredient1", "ingredient2", ...]
  },
  ...
]

Only return the JSON array, no other text.
`, count, ingredientsStr, cuisinesStr)

	c.logger.Info("Requesting dinner suggestions based on %d ingredients and %d cuisines", len(ingredients), len(cuisines))
	c.logger.Debug("OpenAI prompt (first 100 chars): %s", truncateString(prompt, 100))

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a cooking expert who helps families decide what to cook for dinner based on available ingredients.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.7,
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

	var suggestions []map[string]interface{}
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil {
		c.logger.Error("Failed to parse response: %v, Content: %s", err, content)
		return nil, fmt.Errorf("failed to parse OpenAI response: %w", err)
	}

	c.logger.Info("Successfully generated %d dinner suggestions", len(suggestions))
	return suggestions, nil
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

// extractIngredientsFromText extracts ingredients from text using a simple heuristic
// This is a fallback method when JSON parsing fails
func extractIngredientsFromText(s string) []string {
	// Split by common delimiters
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '"' || r == '[' || r == ']' || r == '\t'
	})

	// Clean up and filter
	var ingredients []string
	for _, word := range words {
		word = strings.TrimSpace(word)
		// Skip empty strings and single characters
		if len(word) <= 1 {
			continue
		}
		// Skip common JSON syntax
		if word == "null" || word == "true" || word == "false" {
			continue
		}
		// Skip if it starts with a number (likely part of JSON syntax)
		if len(word) > 0 && (word[0] >= '0' && word[0] <= '9') {
			continue
		}

		ingredients = append(ingredients, word)
	}

	return ingredients
}
