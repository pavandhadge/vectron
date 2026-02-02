package middleware

import (
	"regexp"
	"strings"
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Validation errors
var (
	ErrInvalidEmail  = status.Error(codes.InvalidArgument, "invalid email format")
	ErrWeakPassword  = status.Error(codes.InvalidArgument, "password does not meet security requirements")
	ErrEmptyField    = status.Error(codes.InvalidArgument, "required field is empty")
	ErrTooLong       = status.Error(codes.InvalidArgument, "field exceeds maximum length")
	ErrInvalidFormat = status.Error(codes.InvalidArgument, "field contains invalid characters")
)

// Email validation regex (simplified but effective)
// This regex checks for basic email format: local@domain.tld
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// Common validation constants
const (
	MaxEmailLength      = 254 // RFC 5321
	MaxPasswordLength   = 128 // Prevent DoS via large passwords
	MinPasswordLength   = 8   // NIST minimum recommendation
	MaxNameLength       = 100
	MaxAPIKeyNameLength = 50
)

// ValidateEmail checks if the email format is valid
func ValidateEmail(email string) error {
	if email == "" {
		return ErrEmptyField
	}

	if len(email) > MaxEmailLength {
		return ErrTooLong
	}

	email = strings.ToLower(strings.TrimSpace(email))

	if !emailRegex.MatchString(email) {
		return ErrInvalidEmail
	}

	return nil
}

// PasswordValidationResult contains details about password validation
type PasswordValidationResult struct {
	Valid          bool
	HasMinLength   bool
	HasUppercase   bool
	HasLowercase   bool
	HasDigit       bool
	HasSpecialChar bool
	Errors         []string
}

// ValidatePassword checks if password meets security requirements
// Returns detailed result for UI feedback
func ValidatePassword(password string) *PasswordValidationResult {
	result := &PasswordValidationResult{
		Valid:  true,
		Errors: []string{},
	}

	if password == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "password is required")
		return result
	}

	if len(password) > MaxPasswordLength {
		result.Valid = false
		result.Errors = append(result.Errors, "password is too long (max 128 characters)")
		return result
	}

	if len(password) >= MinPasswordLength {
		result.HasMinLength = true
	} else {
		result.Valid = false
		result.Errors = append(result.Errors, "password must be at least 8 characters long")
	}

	// Check character types
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	result.HasUppercase = hasUpper
	result.HasLowercase = hasLower
	result.HasDigit = hasDigit
	result.HasSpecialChar = hasSpecial

	// Require at least 3 of 4 character types for stronger passwords
	charTypes := 0
	if hasUpper {
		charTypes++
	}
	if hasLower {
		charTypes++
	}
	if hasDigit {
		charTypes++
	}
	if hasSpecial {
		charTypes++
	}

	if charTypes < 3 {
		result.Valid = false
		result.Errors = append(result.Errors, "password must contain at least 3 of: uppercase, lowercase, digits, special characters")
	}

	return result
}

// ValidatePasswordStrict returns an error if password doesn't meet requirements
func ValidatePasswordStrict(password string) error {
	result := ValidatePassword(password)
	if !result.Valid {
		return status.Errorf(codes.InvalidArgument, "password validation failed: %s", strings.Join(result.Errors, "; "))
	}
	return nil
}

// ValidateNotEmpty checks if a required string field is not empty
func ValidateNotEmpty(value, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", fieldName)
	}
	return nil
}

// ValidateMaxLength checks if a string doesn't exceed maximum length
func ValidateMaxLength(value string, maxLength int, fieldName string) error {
	if len(value) > maxLength {
		return status.Errorf(codes.InvalidArgument, "%s exceeds maximum length of %d characters", fieldName, maxLength)
	}
	return nil
}

// ValidateAPIKeyName validates API key name format
func ValidateAPIKeyName(name string) error {
	if err := ValidateNotEmpty(name, "API key name"); err != nil {
		return err
	}
	if err := ValidateMaxLength(name, MaxAPIKeyNameLength, "API key name"); err != nil {
		return err
	}

	// API key names should only contain alphanumeric characters, spaces, hyphens, and underscores
	validNameRegex := regexp.MustCompile(`^[a-zA-Z0-9\s\-_]+$`)
	if !validNameRegex.MatchString(name) {
		return status.Error(codes.InvalidArgument, "API key name can only contain letters, numbers, spaces, hyphens, and underscores")
	}

	return nil
}

// SanitizeString removes potentially dangerous characters and trims whitespace
func SanitizeString(input string) string {
	// Trim whitespace
	sanitized := strings.TrimSpace(input)

	// Remove null bytes
	sanitized = strings.ReplaceAll(sanitized, "\x00", "")

	// Remove control characters except newlines and tabs
	var result strings.Builder
	for _, r := range sanitized {
		if r == '\n' || r == '\t' || r == '\r' || !unicode.IsControl(r) {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// NormalizeEmail normalizes an email address for consistent storage
func NormalizeEmail(email string) string {
	email = strings.TrimSpace(email)
	email = strings.ToLower(email)
	return email
}
