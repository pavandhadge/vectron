// Package rule provides configuration loading and management for rule-based reranking.
package rule

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadConfigFromEnv loads configuration from environment variables.
func LoadConfigFromEnv() Config {
	config := DefaultConfig()
	
	// Load basic weights
	if val := os.Getenv("RULE_EXACT_MATCH_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.ExactMatchBoost = float32(f)
		}
	}
	
	if val := os.Getenv("RULE_FUZZY_MATCH_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.FuzzyMatchBoost = float32(f)
		}
	}
	
	if val := os.Getenv("RULE_TITLE_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.TitleBoost = float32(f)
		}
	}
	
	if val := os.Getenv("RULE_RECENCY_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.RecencyBoost = float32(f)
		}
	}
	
	if val := os.Getenv("RULE_RECENCY_DAYS"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			config.RecencyDays = i
		}
	}
	
	if val := os.Getenv("RULE_TFIDF_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.TFIDFWeight = float32(f)
		}
	}
	
	if val := os.Getenv("RULE_ORIGINAL_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.OriginalWeight = float32(f)
		}
	}
	
	// Load metadata boosts (comma-separated key:value pairs)
	// Example: RULE_METADATA_BOOSTS="verified:0.2,featured:0.3"
	if val := os.Getenv("RULE_METADATA_BOOSTS"); val != "" {
		config.MetadataBoosts = parseKeyValuePairs(val)
	}
	
	// Load metadata penalties
	// Example: RULE_METADATA_PENALTIES="deprecated:0.5,spam:1.0"
	if val := os.Getenv("RULE_METADATA_PENALTIES"); val != "" {
		config.MetadataPenalties = parseKeyValuePairs(val)
	}
	
	// Load stop words (comma-separated)
	if val := os.Getenv("RULE_STOP_WORDS"); val != "" {
		config.StopWords = strings.Split(val, ",")
	}
	
	if val := os.Getenv("RULE_CASE_SENSITIVE"); val != "" {
		config.CaseSensitive = val == "true" || val == "1"
	}
	
	return config
}

// LoadConfigFromJSON loads configuration from a JSON file.
func LoadConfigFromJSON(filepath string) (Config, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("failed to parse config JSON: %w", err)
	}
	
	// Fill in defaults for unset fields
	defaults := DefaultConfig()
	if config.ExactMatchBoost == 0 {
		config.ExactMatchBoost = defaults.ExactMatchBoost
	}
	if config.TFIDFWeight == 0 {
		config.TFIDFWeight = defaults.TFIDFWeight
	}
	if config.OriginalWeight == 0 {
		config.OriginalWeight = defaults.OriginalWeight
	}
	if config.RecencyDays == 0 {
		config.RecencyDays = defaults.RecencyDays
	}
	if len(config.StopWords) == 0 {
		config.StopWords = defaults.StopWords
	}
	
	return config, nil
}

// SaveConfigToJSON saves the configuration to a JSON file.
func SaveConfigToJSON(config Config, filepath string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}

// parseKeyValuePairs parses "key1:value1,key2:value2" format.
func parseKeyValuePairs(input string) map[string]float32 {
	result := make(map[string]float32)
	
	pairs := strings.Split(input, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			if val, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 32); err == nil {
				result[key] = float32(val)
			}
		}
	}
	
	return result
}

// ValidateConfig checks if the configuration values are valid.
func ValidateConfig(config Config) error {
	if config.TFIDFWeight < 0 || config.TFIDFWeight > 1 {
		return fmt.Errorf("TFIDFWeight must be between 0 and 1")
	}
	
	if config.OriginalWeight < 0 || config.OriginalWeight > 1 {
		return fmt.Errorf("OriginalWeight must be between 0 and 1")
	}
	
	if config.RecencyDays < 0 {
		return fmt.Errorf("RecencyDays must be non-negative")
	}
	
	// Warn if weights don't sum close to 1, but don't error
	weightSum := config.TFIDFWeight + config.OriginalWeight
	if weightSum < 0.8 || weightSum > 1.2 {
		// This is acceptable since we add boosts on top
	}
	
	return nil
}

// Example config JSON structure for documentation:
/*
{
  "exact_match_boost": 0.3,
  "fuzzy_match_boost": 0.1,
  "title_boost": 0.2,
  "metadata_boosts": {
    "verified": 0.2,
    "featured": 0.3,
    "high_quality": 0.15
  },
  "metadata_penalties": {
    "deprecated": 0.5,
    "spam": 1.0,
    "low_quality": 0.3
  },
  "recency_boost": 0.15,
  "recency_field": "created_at",
  "recency_days": 30,
  "tfidf_weight": 0.4,
  "original_weight": 0.6,
  "stop_words": ["the", "a", "an", "and", "or"],
  "case_sensitive": false
}
*/
