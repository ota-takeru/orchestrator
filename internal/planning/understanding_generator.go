package planning

import (
	"strings"
)

type UnderstandingInput struct {
	SourceType string
	Title      string
	RawText    string
}

type Assumption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type OpenQuestion struct {
	ID                string `json:"id"`
	Question          string `json:"question"`
	DefaultAssumption string `json:"default_assumption,omitempty"`
}

type AffectedContext struct {
	PRD          []string `json:"prd"`
	Architecture []string `json:"architecture"`
	Roadmap      []string `json:"roadmap"`
	Tasks        []string `json:"tasks"`
}

type RiskAssessment struct {
	Level    string   `json:"level"`
	Reasons  []string `json:"reasons"`
	Blockers []string `json:"blockers,omitempty"`
}

type UnderstandingSnapshot struct {
	InterpretedGoal   []string        `json:"interpreted_goal"`
	UserValue         []string        `json:"user_value"`
	NonGoals          []string        `json:"non_goals"`
	Assumptions       []Assumption    `json:"assumptions"`
	OpenQuestions     []OpenQuestion  `json:"open_questions"`
	AffectedContext   AffectedContext `json:"affected_context"`
	Risk              RiskAssessment  `json:"risk"`
	Confidence        float64         `json:"confidence"`
	RecommendedGoMode string          `json:"recommended_go_mode"`
}

func GenerateUnderstanding(input UnderstandingInput) UnderstandingSnapshot {
	raw := strings.TrimSpace(input.RawText)
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = firstLine(raw)
	}
	if title == "" {
		title = "Initial project understanding"
	}
	level, reasons := ClassifyRisk(raw, input.SourceType)
	mode := RecommendedGoMode(level)
	confidence := 0.74
	if input.SourceType == "initial_concept" {
		confidence = 0.7
	}
	if len(reasons) > 1 {
		confidence = 0.68
	}
	blockers := []string{}
	if level == "L4" {
		blockers = append(blockers, "Explicit human approval is required before implementation because the request touches a high-risk boundary.")
	}
	return UnderstandingSnapshot{
		InterpretedGoal: []string{
			"Deliver the requested outcome: " + title,
			"Keep the implementation focused on the user intent instead of template-driven project assumptions.",
		},
		UserValue: []string{
			"The project owner can see what DevOS believes should be built before work starts.",
			"The coding task can proceed with explicit scope, assumptions, and risk evidence.",
		},
		NonGoals: []string{
			"Do not add unrelated integrations, dependencies, authentication, persistence, or background work unless the request asks for them.",
			"Do not split the work into micro tasks unless a safety, verification, rollback, or human-decision boundary requires it.",
		},
		Assumptions:   assumptionsFor(level, input.SourceType),
		OpenQuestions: openQuestionsFor(level),
		AffectedContext: AffectedContext{
			PRD:          []string{"Product goal and acceptance criteria projection"},
			Architecture: []string{"Implementation direction and risk boundaries"},
			Roadmap:      []string{"Next executable feature chunk"},
			Tasks:        []string{"Feature chunk task proposal"},
		},
		Risk: RiskAssessment{
			Level:    level,
			Reasons:  reasons,
			Blockers: blockers,
		},
		Confidence:        confidence,
		RecommendedGoMode: mode,
	}
}

func ClassifyRisk(raw string, sourceType string) (string, []string) {
	text := strings.ToLower(raw)
	if sourceType == "initial_concept" {
		return "L2", []string{"Initial project direction should be confirmed before artifact projection."}
	}
	if containsAny(text, []string{"db schema", "database schema", "migration", "auth", "oauth", "login", "permission", "external api", "slack", "payment", "billing", "personal data", "個人情報", "secret", ".env", "dependency", "package.json", "go.mod"}) {
		return "L4", []string{"The request appears to touch dependency, schema, auth, external API, secret, payment, or personal-data boundaries."}
	}
	if containsAny(text, []string{"prd", "architecture", "roadmap", "canonical", "設計方針", "方針変更", "acceptance criteria"}) {
		return "L3", []string{"The request may change canonical product or architecture artifacts."}
	}
	if containsAny(text, []string{"ux", "layout", "navigation", "dashboard direction", "体験", "画面方針"}) {
		return "L2", []string{"The request may need a product or UX direction decision before implementation."}
	}
	if containsAny(text, []string{"typo", "copy", "label", "read-only", "誤字", "文言", "表示だけ"}) {
		return "L0", []string{"The request looks like a small read-only or copy-level change."}
	}
	return "L1", []string{"The request looks implementable with safe assumptions and no high-risk boundary detected."}
}

func RecommendedGoMode(level string) string {
	switch level {
	case "L0":
		return "no_gate"
	case "L1":
		return "implement_with_assumptions"
	case "L2":
		return "approval_before_implementation"
	case "L3":
		return "approval_before_canonical_artifact_update"
	case "L4":
		return "hard_gate"
	default:
		return "approval_before_implementation"
	}
}

func ApprovalRequired(level string) bool {
	return level == "L2" || level == "L3" || level == "L4"
}

func assumptionsFor(level string, sourceType string) []Assumption {
	assumptions := []Assumption{
		{ID: "ASM-001", Text: "Use the current approved artifacts and task set as the compatibility baseline."},
		{ID: "ASM-002", Text: "Keep the task as a feature chunk unless a safety boundary requires splitting."},
	}
	if sourceType == "initial_concept" {
		assumptions = append(assumptions, Assumption{ID: "ASM-003", Text: "Generate PRD, Architecture, Roadmap, and Task YAML only after the project understanding is approved."})
	}
	if level == "L1" {
		assumptions = append(assumptions, Assumption{ID: "ASM-004", Text: "Proceed without blocking because no high-risk boundary was detected."})
	}
	if level == "L4" {
		assumptions = append(assumptions, Assumption{ID: "ASM-005", Text: "Do not create ready implementation work until the high-risk boundary is resolved."})
	}
	return assumptions
}

func openQuestionsFor(level string) []OpenQuestion {
	if level == "L0" || level == "L1" {
		return []OpenQuestion{
			{ID: "Q-001", Question: "Are there hidden constraints not visible in the current request?", DefaultAssumption: "No hidden constraints; proceed within existing approved context."},
		}
	}
	return []OpenQuestion{
		{ID: "Q-001", Question: "Is the proposed scope the right boundary for implementation?", DefaultAssumption: "Use the recommended feature chunk scope."},
		{ID: "Q-002", Question: "Should any risk boundary be split into a separate Decision Report?", DefaultAssumption: "Split only L4 boundaries before implementation."},
	}
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return ""
}
