package constants

// AlphanumericRunes contains all alphanumeric characters as runes
var AlphanumericRunes = []rune{
	'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
	'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
	'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
	'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
}

const (
	ContentBlockSize        = 30   // Default number of lines per content block (20-50 range)
	MinProcessedLines       = 10   // Minimum number of valid lines required after processing
	MaxLineLength           = 80   // Maximum line length to match game width
	MaxRetries              = 5    // Maximum number of retries when selecting content blocks
	MaxBlockSize            = 1000 // Maximum number of lines in a content block to prevent memory issues
	CircuitBreakerThreshold = 10   // Number of consecutive failures before circuit breaker trips
)