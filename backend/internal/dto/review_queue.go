package dto

import "time"

// ReviewQueueItem is a read model for the 待复核队列. It intentionally exposes
// only the fields a reviewer needs to triage a draft, plus the computed
// suggestion, waiting time and human-readable ordering reason.
type ReviewQueueItem struct {
	ID              uint      `json:"id"`
	Code            string    `json:"code"`
	Name            string    `json:"name"`
	Facility        string    `json:"facility"`
	Owner           string    `json:"owner"`
	Category        string    `json:"category"`
	RiskLevel       string    `json:"riskLevel"`
	RiskRank        int       `json:"riskRank"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit"`
	PreparedBy      string    `json:"preparedBy"`
	Version         uint      `json:"version"`
	SuggestedLevel  string    `json:"suggestedLevel"`
	QueuedSince     time.Time `json:"queuedSince"`
	WaitingHours    float64   `json:"waitingHours"`
	OrderingReasons []string  `json:"orderingReasons"`
	Order           int       `json:"order"`
}

// ReviewQueueResponse explains why the queue is empty and how the caller's
// own drafts affect the result, so the UI never shows a bare empty table.
type ReviewQueueResponse struct {
	Items          []ReviewQueueItem `json:"items"`
	TotalDrafts    int64             `json:"totalDrafts"`
	VisibleDrafts  int               `json:"visibleDrafts"`
	ExcludedOwn    int64             `json:"excludedOwn"`
	SuggestedLevel string            `json:"suggestedLevel"`
	FilteredOut    int               `json:"filteredOut"`
	EmptyReason    string            `json:"emptyReason,omitempty"`
}

// ReviewQueueQuery carries the optional suggested-level filter. Empty means
// every reviewable draft is returned.
type ReviewQueueQuery struct {
	SuggestedLevel string `form:"suggestedLevel"`
}
