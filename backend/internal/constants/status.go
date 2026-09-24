package constants

// Shared status values are mirrored in frontend/src/types/status.ts. Keeping
// the lists explicit makes state-machine drift visible during code review.

type DefectState string

const (
	DefectStateNew        DefectState = "new"
	DefectStateVerified   DefectState = "verified"
	DefectStateMonitoring DefectState = "monitoring"
	DefectStateMitigated  DefectState = "mitigated"
	DefectStateClosed     DefectState = "closed"
)

var AllDefectState = []string{"new", "verified", "monitoring", "mitigated", "closed"}

type PriorityLevel string

const (
	PriorityLevelObserve  PriorityLevel = "observe"
	PriorityLevelRestrict PriorityLevel = "restrict"
	PriorityLevelUrgent   PriorityLevel = "urgent"
)

var AllPriorityLevel = []string{"observe", "restrict", "urgent"}

// Risk levels classify 优先级决定 drafts. They are mirrored in the frontend
// SeverityBadge and drive the review queue ordering and suggestion logic.
var (
	RiskLevelLow      = "low"
	RiskLevelMedium   = "medium"
	RiskLevelHigh     = "high"
	RiskLevelCritical = "critical"
)

var AllRiskLevel = []string{"low", "medium", "high", "critical"}

// RiskRank orders risk levels from least to most urgent. Unknown levels rank
// last so malformed data never overtakes a known high-risk draft.
var RiskRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

// RiskToSuggestedLevel maps a draft risk level to the 处置优先级 level an
// independent reviewer is most likely to confirm.
var RiskToSuggestedLevel = map[string]string{
	"low": string(PriorityLevelObserve), "medium": string(PriorityLevelObserve),
	"high": string(PriorityLevelRestrict), "critical": string(PriorityLevelUrgent),
}

// IsPriorityLevel reports whether level is a valid final 处置优先级 level.
func IsPriorityLevel(level string) bool {
	for _, candidate := range AllPriorityLevel {
		if candidate == level {
			return true
		}
	}
	return false
}

// SuggestedLevelForRisk returns the review suggestion for a risk level,
// defaulting to the most urgent level when the level is unknown.
func SuggestedLevelForRisk(riskLevel string) string {
	if level, ok := RiskToSuggestedLevel[riskLevel]; ok {
		return level
	}
	return string(PriorityLevelUrgent)
}

// RankRisk returns the urgency rank of a risk level; unknown levels rank zero.
func RankRisk(riskLevel string) int {
	return RiskRank[riskLevel]
}

var BridgeAssetTransitions = map[string]map[string]bool{
	"active":     {"restricted": true, "closed": true},
	"restricted": {"closed": true, "retired": true, "active": true},
	"closed":     {"retired": true, "restricted": true},
	"retired":    {"closed": true},
}

var InspectionRoundTransitions = map[string]map[string]bool{
	"planned":   {"running": true, "review": true},
	"running":   {"review": true, "completed": true, "planned": true},
	"review":    {"completed": true, "running": true},
	"completed": {"review": true},
}

var DefectFindingTransitions = map[string]map[string]bool{
	"new":        {"verified": true, "monitoring": true},
	"verified":   {"monitoring": true, "mitigated": true, "new": true},
	"monitoring": {"mitigated": true, "closed": true, "verified": true},
	"mitigated":  {"closed": true, "monitoring": true},
	"closed":     {"mitigated": true},
}

var PriorityDecisionTransitions = map[string]map[string]bool{
	"draft":    {"observe": true, "restrict": true, "urgent": true},
	"observe":  {},
	"restrict": {},
	"urgent":   {},
}

func CanTransition(graph map[string]map[string]bool, from, to string) bool {
	targets, exists := graph[from]
	return exists && targets[to]
}
