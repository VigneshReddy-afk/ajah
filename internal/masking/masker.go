package masking

import (
	"regexp"

	"go.uber.org/zap"
)

// MaskResult holds the original text, the masked text, and metadata about
// every PII span that was detected.
type MaskResult struct {
	Original    string
	Masked      string
	DetectedPII []PIIMatch
	WasMasked   bool
}

// PIIMatch describes a single detected PII span within the text that was
// passed to Mask at the time that pattern was evaluated.
type PIIMatch struct {
	Type  string
	Value string
	Start int
	End   int
}

type piiPattern struct {
	name        string
	re          *regexp.Regexp
	replacement string
}

// Masker detects and redacts PII from free-form text.
// All compiled regexes are read-only after construction, so Mask is safe
// to call concurrently from multiple goroutines without additional locking.
type Masker struct {
	logger   *zap.Logger
	patterns []piiPattern
}

// New returns a Masker with all PII patterns pre-compiled.
func New(logger *zap.Logger) *Masker {
	return &Masker{
		logger: logger,
		patterns: []piiPattern{
			{
				// API keys must come first so a key like sk-abc… is not
				// partially consumed by more general patterns.
				name: "API_KEY",
				re:   regexp.MustCompile(`\b(?:sk|pk|key)-[A-Za-z0-9]{20,}\b`),
				replacement: "[API_KEY]",
			},
			{
				// 13-19 digit card numbers, optionally grouped with spaces/dashes.
				// First alternative: four groups of 4 digits (16-digit form).
				// Second alternative: plain 13-19 digit run.
				name: "CREDIT_CARD",
				re:   regexp.MustCompile(`\b(?:\d{4}[ -]?\d{4}[ -]?\d{4}[ -]?\d{1,7}|\d{13,19})\b`),
				replacement: "[CREDIT_CARD]",
			},
			{
				name:        "SSN",
				re:          regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
				replacement: "[SSN]",
			},
			{
				name:        "EMAIL",
				re:          regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
				replacement: "[EMAIL]",
			},
			{
				// Matches:
				//   +1-555-123-4567
				//   (555) 123-4567
				//   555-123-4567
				// Separators may be -, ., or a space.
				name:        "PHONE",
				re:          regexp.MustCompile(`(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}`),
				replacement: "[PHONE]",
			},
			{
				// IPv4 — applied last because digit groups with dots could
				// interfere with less-specific patterns.
				name:        "IP_ADDRESS",
				re:          regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
				replacement: "[IP_ADDRESS]",
			},
		},
	}
}

// Mask scans text for PII, replaces every detected span with a labelled
// placeholder, and returns a MaskResult describing what was found.
//
// Masks are applied in the order: API_KEY → CREDIT_CARD → SSN → EMAIL →
// PHONE → IP_ADDRESS. The Start/End offsets in each PIIMatch are positions
// within the intermediate text at the point that pattern was evaluated.
//
// Mask is safe for concurrent use.
func (m *Masker) Mask(text string) MaskResult {
	result := MaskResult{
		Original: text,
		Masked:   text,
	}

	for _, p := range m.patterns {
		locs := p.re.FindAllStringIndex(result.Masked, -1)
		if len(locs) == 0 {
			continue
		}
		for _, loc := range locs {
			result.DetectedPII = append(result.DetectedPII, PIIMatch{
				Type:  p.name,
				Value: result.Masked[loc[0]:loc[1]],
				Start: loc[0],
				End:   loc[1],
			})
		}
		result.Masked = p.re.ReplaceAllString(result.Masked, p.replacement)
	}

	if len(result.DetectedPII) > 0 {
		result.WasMasked = true
		m.logger.Warn("PII detected in text",
			zap.Strings("pii_types", uniqueTypes(result.DetectedPII)),
		)
	}

	return result
}

func uniqueTypes(matches []PIIMatch) []string {
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m.Type]; !ok {
			seen[m.Type] = struct{}{}
			out = append(out, m.Type)
		}
	}
	return out
}
