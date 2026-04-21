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

	if val := os.Getenv("RULE_EXACT_MATCH_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.ExactMatchBoost = float32(f)
		}
	}

	if val := os.Getenv("RULE_FUZZY_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.FuzzyBoost = float32(f)
		}
	}

	if val := os.Getenv("RULE_TITLE_BOOST"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.TitleBoost = float32(f)
		}
	}

	if val := os.Getenv("RULE_RECENCY_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.RecencyWeight = float32(f)
		}
	}

	if val := os.Getenv("RULE_RECENCY_DAYS"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			config.RecencyDays = i
		}
	}

	if val := os.Getenv("RULE_HALF_LIFE_DAYS"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.HalfLifeDays = float64(f)
		}
	}

	if val := os.Getenv("RULE_ORIGINAL_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.OriginalWeight = float32(f)
		}
	}

	if val := os.Getenv("RULE_BM25_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.BM25Weight = float32(f)
		}
	}

	if val := os.Getenv("RULE_KEYWORD_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.KeywordWeight = float32(f)
		}
	}

	if val := os.Getenv("RULE_COVERAGE_WEIGHT"); val != "" {
		if f, err := strconv.ParseFloat(val, 32); err == nil {
			config.CoverageWeight = float32(f)
		}
	}

	if val := os.Getenv("RULE_METADATA_BOOSTS"); val != "" {
		config.MetadataBoosts = parseKeyValuePairs(val)
	}

	if val := os.Getenv("RULE_METADATA_PENALTIES"); val != "" {
		config.MetadataPenalties = parseKeyValuePairs(val)
	}

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

	defaults := DefaultConfig()
	if config.ExactMatchBoost == 0 {
		config.ExactMatchBoost = defaults.ExactMatchBoost
	}
	if config.BM25Weight == 0 {
		config.BM25Weight = defaults.BM25Weight
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
	totalWeight := config.OriginalWeight + config.BM25Weight + config.KeywordWeight + config.CoverageWeight + config.RecencyWeight
	if totalWeight < 0 || totalWeight > 2 {
		return fmt.Errorf("weights sum should be between 0 and 2")
	}

	if config.RecencyDays < 0 {
		return fmt.Errorf("RecencyDays must be non-negative")
	}

	if config.BM25K1 < 0 {
		return fmt.Errorf("BM25K1 must be non-negative")
	}

	if config.BM25B < 0 || config.BM25B > 1 {
		return fmt.Errorf("BM25B must be between 0 and 1")
	}

	return nil
}