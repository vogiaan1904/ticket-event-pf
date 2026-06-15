package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Examples:
//   - "Taylor Swift Eras Tour 2024" -> TB-TSE24-20251008-A3B7K9M2
//   - "Rock Concert" -> TB-RC-20251008-A3B7K9M2
//   - "Jazz Festival 2024" -> TB-JF24-20251008-A3B7K9M2
//   - "Coldplay" -> TB-COLDPL-20251008-A3B7K9M2

// GenerateOrderCode generates a unique, user-friendly order code
// Format: TB-YYYYMMDD-XXXXXXXX (e.g., TB-20251008-A3B7K9M2)
// - TB: TicketBottle brand prefix
// - YYYYMMDD: Date for easy sorting and identification
// - XXXXXXXX: 8-character alphanumeric code (30^8 = 656 billion combinations)
func GenerateOrderCode() string {
	dateStr := time.Now().Format("20060102")

	charset := "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLength := 8

	var code strings.Builder
	for i := 0; i < codeLength; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		code.WriteByte(charset[n.Int64()])
	}

	return fmt.Sprintf("TB-%s-%s", dateStr, code.String())
}

// GenerateOrderCodeWithEventPrefix generates a unique order code with event-specific prefix
// Format: TB-PREFIX-YYYYMMDD-XXXXXXXX
// Examples:
//   - "Taylor Swift Eras Tour 2024" -> TB-TSE24-20251008-A3B7K9M2
//   - "Rock Concert" -> TB-RC-20251008-A3B7K9M2
//   - "Jazz Festival 2024" -> TB-JF24-20251008-A3B7K9M2
//   - "Coldplay" -> TB-COLDPL-20251008-A3B7K9M2
//
// The prefix is generated from the event name by taking initials and removing common words
func GenerateOrderCodeWithEventPrefix(eventName string) string {
	dateStr := time.Now().Format("20060102")

	charset := "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLength := 8

	var code strings.Builder
	for i := 0; i < codeLength; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		code.WriteByte(charset[n.Int64()])
	}

	eventPrefix := generateEventPrefix(eventName)

	return fmt.Sprintf("TB-%s-%s-%s", eventPrefix, dateStr, code.String())
}

func generateEventPrefix(eventName string) string {
	commonWords := []string{"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for", "of", "with", "by", "concert", "show", "event", "festival", "tour"}

	eventName = strings.ToLower(strings.TrimSpace(eventName))
	words := strings.Fields(eventName)

	var meaningfulWords []string
	for _, word := range words {
		isCommon := false
		for _, common := range commonWords {
			if word == common {
				isCommon = true
				break
			}
		}
		if !isCommon && len(word) > 1 {
			meaningfulWords = append(meaningfulWords, word)
		}
	}

	var prefix strings.Builder

	if len(meaningfulWords) == 0 {
		cleaned := strings.ReplaceAll(strings.ToUpper(eventName), " ", "")
		if len(cleaned) > 6 {
			return cleaned[:6]
		}
		return cleaned
	}

	if len(meaningfulWords) == 1 {
		word := strings.ToUpper(meaningfulWords[0])
		if len(word) > 6 {
			return word[:6]
		}
		return word
	}

	for _, word := range meaningfulWords {
		if len(word) > 0 {
			hasNumber := false
			for _, char := range word {
				if char >= '0' && char <= '9' {
					hasNumber = true
					break
				}
			}

			if hasNumber {
				numPart := ""
				for _, char := range word {
					if char >= '0' && char <= '9' && len(numPart) < 4 {
						numPart += string(char)
					}
				}
				prefix.WriteString(numPart)
			} else {
				prefix.WriteByte(byte(strings.ToUpper(string(word[0]))[0]))
			}

			if prefix.Len() >= 6 {
				break
			}
		}
	}

	result := prefix.String()
	if len(result) > 6 {
		return result[:6]
	}

	if len(result) < 4 && len(meaningfulWords) > 0 {
		for _, word := range meaningfulWords {
			if len(result) >= 6 {
				break
			}
			for _, char := range strings.ToUpper(word) {
				if len(result) >= 6 {
					break
				}
				if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
					if !strings.ContainsRune(result, char) {
						result += string(char)
					}
				}
			}
		}
	}

	if len(result) < 3 {
		result = "EVT" + result
	}

	return result
}

// GenerateShortOrderCode generates a shorter order code for space-constrained displays
// Format: TB-XXXXXXXX (e.g., TB-A3B7K9M2)
func GenerateShortOrderCode() string {
	charset := "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLength := 8

	var code strings.Builder
	for i := 0; i < codeLength; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		code.WriteByte(charset[n.Int64()])
	}

	return fmt.Sprintf("TB-%s", code.String())
}

// ValidateOrderCode validates if an order code follows the expected format
func ValidateOrderCode(code string) bool {
	if !strings.HasPrefix(code, "TB-") {
		return false
	}

	remaining := code[3:]

	parts := strings.Split(remaining, "-")

	switch len(parts) {
	case 1:
		return len(parts[0]) == 8 && isValidCharset(parts[0])
	case 2:
		return len(parts[0]) == 8 && isValidDate(parts[0]) &&
			len(parts[1]) == 8 && isValidCharset(parts[1])
	case 3:
		return len(parts[0]) <= 8 &&
			len(parts[1]) == 8 && isValidDate(parts[1]) &&
			len(parts[2]) == 8 && isValidCharset(parts[2])
	default:
		return false
	}
}

func isValidCharset(s string) bool {
	charset := "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	for _, char := range s {
		if !strings.ContainsRune(charset, char) {
			return false
		}
	}
	return true
}

func isValidDate(s string) bool {
	_, err := time.Parse("20060102", s)
	return err == nil
}
