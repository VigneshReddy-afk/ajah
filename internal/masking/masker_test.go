package masking

import (
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func newMasker() *Masker { return New(zap.NewNop()) }

// ---- per-type detection tests -----------------------------------------------

func TestMask_Email(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"send to user@example.com please", "[EMAIL]"},
		{"multi: a@b.io and c@d.co.uk here", "[EMAIL]"},
		{"no-reply+tag@sub.domain.org", "[EMAIL]"},
	}
	m := newMasker()
	for _, tc := range cases {
		result := m.Mask(tc.input)
		if !result.WasMasked {
			t.Errorf("input %q: WasMasked = false, want true", tc.input)
		}
		if strings.Contains(result.Masked, "@") {
			t.Errorf("input %q: masked output still contains '@': %s", tc.input, result.Masked)
		}
		if !strings.Contains(result.Masked, tc.want) {
			t.Errorf("input %q: masked output missing %s: %s", tc.input, tc.want, result.Masked)
		}
		assertPIIType(t, result, "EMAIL")
	}
}

func TestMask_Phone(t *testing.T) {
	t.Parallel()
	cases := []string{
		"+1-555-123-4567",
		"(555) 123-4567",
		"555-123-4567",
		"+1 800 555 1234",
	}
	m := newMasker()
	for _, input := range cases {
		result := m.Mask("Call us at " + input + " now")
		if !result.WasMasked {
			t.Errorf("input %q: WasMasked = false, want true", input)
		}
		if !strings.Contains(result.Masked, "[PHONE]") {
			t.Errorf("input %q: masked output missing [PHONE]: %s", input, result.Masked)
		}
		assertPIIType(t, result, "PHONE")
	}
}

func TestMask_CreditCard(t *testing.T) {
	t.Parallel()
	cases := []string{
		"4111111111111111",       // 16 digits plain
		"4111 1111 1111 1111",   // 16 digits with spaces
		"4111-1111-1111-1111",   // 16 digits with dashes
		"371449635398431",       // 15 digits (Amex)
		"4111111111111",         // 13 digits (old Visa)
	}
	m := newMasker()
	for _, input := range cases {
		result := m.Mask("Card: " + input + " end")
		if !result.WasMasked {
			t.Errorf("card %q: WasMasked = false, want true", input)
		}
		if !strings.Contains(result.Masked, "[CREDIT_CARD]") {
			t.Errorf("card %q: masked output missing [CREDIT_CARD]: %s", input, result.Masked)
		}
		assertPIIType(t, result, "CREDIT_CARD")
	}
}

func TestMask_SSN(t *testing.T) {
	t.Parallel()
	cases := []string{
		"123-45-6789",
		"000-00-0001",
	}
	m := newMasker()
	for _, input := range cases {
		result := m.Mask("SSN is " + input + " here")
		if !result.WasMasked {
			t.Errorf("SSN %q: WasMasked = false, want true", input)
		}
		if !strings.Contains(result.Masked, "[SSN]") {
			t.Errorf("SSN %q: masked output missing [SSN]: %s", input, result.Masked)
		}
		assertPIIType(t, result, "SSN")
	}
}

func TestMask_IPAddress(t *testing.T) {
	t.Parallel()
	cases := []string{
		"192.168.1.1",
		"10.0.0.1",
		"255.255.255.0",
		"0.0.0.0",
	}
	m := newMasker()
	for _, input := range cases {
		result := m.Mask("source IP: " + input + " end")
		if !result.WasMasked {
			t.Errorf("IP %q: WasMasked = false, want true", input)
		}
		if !strings.Contains(result.Masked, "[IP_ADDRESS]") {
			t.Errorf("IP %q: masked output missing [IP_ADDRESS]: %s", input, result.Masked)
		}
		assertPIIType(t, result, "IP_ADDRESS")
	}
}

func TestMask_APIKey(t *testing.T) {
	t.Parallel()
	cases := []string{
		"sk-abcdefghijklmnopqrstu",          // sk- + 21 chars
		"pk-ABCDEFGHIJKLMNOPQRSTU12345",     // pk- + 25 chars
		"key-abcdefghijklmnopqrstuvwxyz123", // key- + 29 chars
	}
	m := newMasker()
	for _, input := range cases {
		result := m.Mask("token=" + input + " end")
		if !result.WasMasked {
			t.Errorf("key %q: WasMasked = false, want true", input)
		}
		if !strings.Contains(result.Masked, "[API_KEY]") {
			t.Errorf("key %q: masked output missing [API_KEY]: %s", input, result.Masked)
		}
		assertPIIType(t, result, "API_KEY")
	}
}

func TestMask_ShortAPIKeyNotMasked(t *testing.T) {
	t.Parallel()
	m := newMasker()
	// Only 10 alphanumeric chars after sk- — below the 20-char threshold.
	result := m.Mask("key is sk-shortkey12")
	// Should NOT be detected as API_KEY.
	for _, match := range result.DetectedPII {
		if match.Type == "API_KEY" {
			t.Errorf("short key incorrectly detected as API_KEY")
		}
	}
}

// ---- clean text -------------------------------------------------------------

func TestMask_CleanTextNotMasked(t *testing.T) {
	t.Parallel()
	m := newMasker()
	inputs := []string{
		"Hello, world!",
		"The quick brown fox jumps over the lazy dog.",
		"Order total: $42.00",
		"Version 1.2.3 released today",
	}
	for _, input := range inputs {
		result := m.Mask(input)
		if result.WasMasked {
			t.Errorf("input %q: WasMasked = true, want false", input)
		}
		if len(result.DetectedPII) != 0 {
			t.Errorf("input %q: DetectedPII = %v, want empty", input, result.DetectedPII)
		}
		if result.Masked != result.Original {
			t.Errorf("input %q: Masked != Original", input)
		}
	}
}

// ---- multiple PII types in one string ---------------------------------------

func TestMask_MultiplePIITypes(t *testing.T) {
	t.Parallel()
	m := newMasker()

	text := "email user@example.com, SSN 123-45-6789, IP 10.0.0.1, phone 555-867-5309"
	result := m.Mask(text)

	if !result.WasMasked {
		t.Fatal("WasMasked = false, want true")
	}

	for _, placeholder := range []string{"[EMAIL]", "[SSN]", "[IP_ADDRESS]", "[PHONE]"} {
		if !strings.Contains(result.Masked, placeholder) {
			t.Errorf("masked output missing %s: %s", placeholder, result.Masked)
		}
	}

	for _, original := range []string{"user@example.com", "123-45-6789", "10.0.0.1", "555-867-5309"} {
		if strings.Contains(result.Masked, original) {
			t.Errorf("masked output still contains original PII %q", original)
		}
	}

	types := make(map[string]bool)
	for _, match := range result.DetectedPII {
		types[match.Type] = true
	}
	for _, want := range []string{"EMAIL", "SSN", "IP_ADDRESS", "PHONE"} {
		if !types[want] {
			t.Errorf("DetectedPII missing type %q", want)
		}
	}
}

// ---- original value never leaks ---------------------------------------------

func TestMask_OutputNeverContainsOriginalPII(t *testing.T) {
	t.Parallel()
	m := newMasker()

	piiValues := []string{
		"user@example.com",
		"123-45-6789",
		"4111111111111111",
		"192.168.1.1",
		"+1-555-123-4567",
		"sk-abcdefghijklmnopqrstu",
	}

	for _, pii := range piiValues {
		result := m.Mask("data: " + pii + " end")
		if strings.Contains(result.Masked, pii) {
			t.Errorf("masked output still contains original PII %q: got %s", pii, result.Masked)
		}
	}
}

// ---- Original field is unchanged --------------------------------------------

func TestMask_OriginalFieldUnchanged(t *testing.T) {
	t.Parallel()
	m := newMasker()
	input := "contact user@example.com for details"
	result := m.Mask(input)
	if result.Original != input {
		t.Errorf("Original = %q, want %q", result.Original, input)
	}
}

// ---- concurrent safety ------------------------------------------------------

func TestMask_Concurrent(t *testing.T) {
	t.Parallel()
	m := newMasker()

	texts := []string{
		"email user@example.com here",
		"SSN is 123-45-6789",
		"IP 192.168.0.1 connected",
		"call 555-123-4567 now",
		"card 4111 1111 1111 1111 used",
		"key sk-abcdefghijklmnopqrstu active",
		"no pii here at all",
	}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := texts[i%len(texts)]
			result := m.Mask(text)
			_ = result.WasMasked
		}(i)
	}
	wg.Wait()
}

// ---- helpers ----------------------------------------------------------------

func assertPIIType(t *testing.T, result MaskResult, wantType string) {
	t.Helper()
	for _, match := range result.DetectedPII {
		if match.Type == wantType {
			return
		}
	}
	t.Errorf("DetectedPII does not contain type %q; got %v", wantType, result.DetectedPII)
}
