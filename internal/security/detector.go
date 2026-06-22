package security

import (
    "regexp"
    "strings"
)

// ThreatKind identifies the category of a detected security threat.
type ThreatKind string

const (
    ThreatPromptInjection ThreatKind = "prompt_injection"
    ThreatJailbreak       ThreatKind = "jailbreak"
    ThreatDataExfiltration ThreatKind = "data_exfiltration"
)

// ThreatMatch describes a single detected threat within the prompt.
type ThreatMatch struct {
    Kind    ThreatKind
    Pattern string
    Snippet string // up to 80 chars around the match
}

// ScanResult is the output of a Detector.Scan call.
type ScanResult struct {
    Threats      []ThreatMatch
    InjectionRisk float64 // 0.0–1.0
    JailbreakRisk float64 // 0.0–1.0
    ExfilRisk     float64 // 0.0–1.0
    Verdict       string  // "clean", "suspicious", "blocked"
    ShouldBlock   bool
}

type pattern struct {
    kind    ThreatKind
    label   string
    re      *regexp.Regexp
    weight  float64 // contribution to risk score per match (capped at 1.0)
}

// Detector scans prompt text for injection, jailbreak, and data-exfiltration patterns.
// All compiled regexes are read-only after construction — Scan is safe to call concurrently.
type Detector struct {
    patterns    []pattern
    blockThresh float64 // combined risk threshold above which ShouldBlock is true
}

// New returns a Detector ready for use.
// blockThreshold: 0.0 disables blocking entirely; 0.7 is a sensible default.
func New(blockThreshold float64) *Detector {
    return &Detector{
        blockThresh: blockThreshold,
        patterns: []pattern{
            // ── Prompt injection ──────────────────────────────────────────────
            {
                kind:   ThreatPromptInjection,
                label:  "ignore_previous",
                re:     regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|directives?|context|rules?)`),
                weight: 0.9,
            },
            {
                kind:   ThreatPromptInjection,
                label:  "disregard_instructions",
                re:     regexp.MustCompile(`(?i)(disregard|forget|override|bypass|skip)\s+(all\s+)?(your\s+)?(instructions?|rules?|guidelines?|constraints?|limitations?|restrictions?)`),
                weight: 0.85,
            },
            {
                kind:   ThreatPromptInjection,
                label:  "new_instructions",
                re:     regexp.MustCompile(`(?i)(new|updated|revised|actual|real)\s+instructions?\s*[:：]`),
                weight: 0.75,
            },
            {
                kind:   ThreatPromptInjection,
                label:  "system_override",
                re:     regexp.MustCompile(`(?i)\[?(system|sys)\]?\s*[:：]\s*(ignore|you are now|disregard|override)`),
                weight: 0.9,
            },
            {
                kind:   ThreatPromptInjection,
                label:  "end_of_prompt_marker",
                re:     regexp.MustCompile(`(?i)(---+|===+|###)\s*(end\s+of\s+(prompt|instructions?|context)|new\s+task|actual\s+task|real\s+instructions?)\s*(---+|===+|###)?`),
                weight: 0.8,
            },
            {
                kind:   ThreatPromptInjection,
                label:  "inject_via_translation",
                re:     regexp.MustCompile(`(?i)(translate|translat\w+).{0,40}(ignore|disregard|forget)\s+(previous|prior|above)`),
                weight: 0.85,
            },

            // ── Jailbreak ─────────────────────────────────────────────────────
            {
                kind:   ThreatJailbreak,
                label:  "dan_pattern",
                re:     regexp.MustCompile(`(?i)\b(DAN|do\s+anything\s+now|jailbreak(ed)?|jail\s*break)\b`),
                weight: 0.95,
            },
            {
                kind:   ThreatJailbreak,
                label:  "developer_mode",
                re:     regexp.MustCompile(`(?i)(developer\s+mode|dev\s+mode|debug\s+mode)\s+(enabled|activated|on|unlocked)`),
                weight: 0.9,
            },
            {
                kind:   ThreatJailbreak,
                label:  "no_restrictions",
                re:     regexp.MustCompile(`(?i)(pretend|act|behave|respond)\s+(as\s+if|like)\s+(you\s+)?(have\s+)?(no\s+)?(restrictions?|limits?|rules?|guidelines?|ethics?|filters?|constraints?)`),
                weight: 0.85,
            },
            {
                kind:   ThreatJailbreak,
                label:  "you_are_now",
                re:     regexp.MustCompile(`(?i)you\s+are\s+now\s+(a|an)?\s*(unrestricted|uncensored|unfiltered|evil|malicious|hacker|villain)`),
                weight: 0.9,
            },
            {
                kind:   ThreatJailbreak,
                label:  "fictional_framing_escape",
                re:     regexp.MustCompile(`(?i)(in\s+(this\s+)?fiction|for\s+a\s+(story|novel|game|roleplay)|hypothetically\s+speaking).{0,60}(how\s+to\s+(make|build|create|synthesize|hack)|step.by.step)`),
                weight: 0.8,
            },
            {
                kind:   ThreatJailbreak,
                label:  "token_smuggling",
                re:     regexp.MustCompile(`(?i)(base64|rot13|hex.encoded|encoded\s+version)\s*(of|for)?.{0,30}(instructions?|prompt|command|payload)`),
                weight: 0.85,
            },
            {
                kind:   ThreatJailbreak,
                label:  "grandma_exploit",
                re:     regexp.MustCompile(`(?i)(my\s+)?(grandma|grandmother|late\s+\w+)\s+(used\s+to\s+)?(tell|read|recite|explain).{0,60}(how\s+to|instructions?\s+for|steps?\s+to)`),
                weight: 0.75,
            },

            // ── Data exfiltration ─────────────────────────────────────────────
            {
                kind:   ThreatDataExfiltration,
                label:  "reveal_system_prompt",
                re:     regexp.MustCompile(`(?i)(print|show|display|output|repeat|reveal|tell\s+me|what\s+is)\s+(your\s+)?(system\s+prompt|initial\s+prompt|base\s+prompt|original\s+instructions?|context\s+window|full\s+prompt)`),
                weight: 0.9,
            },
            {
                kind:   ThreatDataExfiltration,
                label:  "training_data_probe",
                re:     regexp.MustCompile(`(?i)(what\s+(data|information|content)\s+(were\s+you|was\s+used)\s+trained\s+on|repeat\s+your\s+training\s+data|verbatim.{0,20}training)`),
                weight: 0.8,
            },
            {
                kind:   ThreatDataExfiltration,
                label:  "other_users_data",
                re:     regexp.MustCompile(`(?i)(what\s+(did|do|have)\s+(other\s+)?(users?|people|customers?)\s+(ask|say|query|request|tell)|previous\s+(user|customer|conversation)s?\s+(said|asked|told|shared))`),
                weight: 0.85,
            },
            {
                kind:   ThreatDataExfiltration,
                label:  "api_key_probe",
                re:     regexp.MustCompile(`(?i)(what\s+is\s+(your|the)\s+(api\s+key|secret|token|password|credential)|print\s+(your\s+)?(api\s+key|access\s+token|secret\s+key))`),
                weight: 0.95,
            },
            {
                kind:   ThreatDataExfiltration,
                label:  "memory_dump",
                re:     regexp.MustCompile(`(?i)(dump\s+(your\s+)?(memory|context|prompt|state)|output\s+everything\s+(you\s+know|in\s+your\s+context|from\s+your\s+memory))`),
                weight: 0.85,
            },
        },
    }
}

// Scan checks prompt text for security threats and returns a ScanResult.
// Safe for concurrent use.
func (d *Detector) Scan(prompt string) ScanResult {
    if strings.TrimSpace(prompt) == "" {
        return ScanResult{Verdict: "clean"}
    }

    var injMatches, jailMatches, exfilMatches []ThreatMatch
    var injScore, jailScore, exfilScore float64

    for _, p := range d.patterns {
        locs := p.re.FindStringIndex(prompt)
        if locs == nil {
            continue
        }

        start := locs[0]
        end := locs[1]
        snippetStart := max(0, start-20)
        snippetEnd := min(len(prompt), end+20)
        snippet := strings.TrimSpace(prompt[snippetStart:snippetEnd])
        if len(snippet) > 80 {
            snippet = snippet[:80] + "…"
        }

        match := ThreatMatch{
            Kind:    p.kind,
            Pattern: p.label,
            Snippet: snippet,
        }

        switch p.kind {
        case ThreatPromptInjection:
            injMatches = append(injMatches, match)
            injScore = min(1.0, injScore+p.weight)
        case ThreatJailbreak:
            jailMatches = append(jailMatches, match)
            jailScore = min(1.0, jailScore+p.weight)
        case ThreatDataExfiltration:
            exfilMatches = append(exfilMatches, match)
            exfilScore = min(1.0, exfilScore+p.weight)
        }
    }

    allThreats := append(append(injMatches, jailMatches...), exfilMatches...)
    combined := max(injScore, max(jailScore, exfilScore))

    verdict := "clean"
    if combined >= 0.5 {
        verdict = "suspicious"
    }
    shouldBlock := false
    if d.blockThresh > 0 && combined >= d.blockThresh {
        verdict = "blocked"
        shouldBlock = true
    }

    return ScanResult{
        Threats:       allThreats,
        InjectionRisk: injScore,
        JailbreakRisk: jailScore,
        ExfilRisk:     exfilScore,
        Verdict:       verdict,
        ShouldBlock:   shouldBlock,
    }
}
