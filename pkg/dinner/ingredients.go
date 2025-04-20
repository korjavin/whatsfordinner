package dinner

import (
	"strings"
)

// CompareIngredients compares the ingredients needed for a dish with what's in the fridge
// Returns a list of missing ingredients
func CompareIngredients(neededIngredients []string, fridgeIngredients []string) []string {
	// Convert all ingredients to lowercase for case-insensitive comparison
	normalizedFridge := make(map[string]bool)
	for _, ingredient := range fridgeIngredients {
		normalizedFridge[normalizeIngredient(ingredient)] = true
	}
	
	// Check which ingredients are missing
	var missingIngredients []string
	for _, ingredient := range neededIngredients {
		normalized := normalizeIngredient(ingredient)
		
		// Check if the ingredient or a similar one is in the fridge
		found := false
		for fridgeIngredient := range normalizedFridge {
			if strings.Contains(fridgeIngredient, normalized) || strings.Contains(normalized, fridgeIngredient) {
				found = true
				break
			}
		}
		
		if !found {
			missingIngredients = append(missingIngredients, ingredient)
		}
	}
	
	return missingIngredients
}

// normalizeIngredient normalizes an ingredient name for comparison
func normalizeIngredient(ingredient string) string {
	// Remove quantity if present
	if idx := strings.Index(ingredient, "("); idx > 0 {
		ingredient = ingredient[:idx]
	}
	
	// Convert to lowercase and trim spaces
	return strings.ToLower(strings.TrimSpace(ingredient))
}
