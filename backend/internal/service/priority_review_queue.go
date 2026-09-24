package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
)

// PriorityReviewItem is one draft decision in a reviewer's pending queue. It
// carries the derived suggestion, waiting time and a human-readable reason so
// reviewers can see why the draft is ranked at its position.
type PriorityReviewItem struct {
	ID             uint      `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Facility       string    `json:"facility"`
	Owner          string    `json:"owner"`
	Category       string    `json:"category"`
	RiskLevel      string    `json:"riskLevel"`
	RiskRank       int       `json:"riskRank"`
	MetricValue    float64   `json:"metricValue"`
	MetricUnit     string    `json:"metricUnit"`
	RelatedCode    string    `json:"relatedCode"`
	PreparedBy     string    `json:"preparedBy"`
	Version        uint      `json:"version"`
	UpdatedAt      time.Time `json:"updatedAt"`
	SuggestedLevel string    `json:"suggestedLevel"`
	WaitingHours   float64   `json:"waitingHours"`
	Rank           int       `json:"rank"`
	SortReason     string    `json:"sortReason"`
}

// PriorityReviewQueue is the ranked worklist for one reviewer. Counts are kept
// even when the visible list is empty so the page can explain why.
type PriorityReviewQueue struct {
	Items            []PriorityReviewItem `json:"items"`
	Filter           string               `json:"filter,omitempty"`
	TotalDrafts      int                  `json:"totalDrafts"`
	ExcludedOwnCount int                  `json:"excludedOwnCount"`
	QueueCount       int                  `json:"queueCount"`
	FilteredCount    int                  `json:"filteredCount"`
	EmptyReason      string               `json:"emptyReason,omitempty"`
}

// Risk ranks used by the queue. Higher means more dangerous. Unknown risk
// values sort below low so they never overtake a declared risk level.
var reviewRiskRank = map[string]int{
	"low": 1, "medium": 2, "high": 3, "critical": 4,
}

var reviewRiskLabels = map[string]string{
	"low": "低", "medium": "中", "high": "高", "critical": "严重",
}

const (
	// Waiting thresholds follow the internal SLA: a review is expected within
	// two days and a high-risk draft must not wait longer than three.
	restrictWaitHours = 48
	urgentWaitHours   = 72
	highMetricValue   = 60
)

// ReviewQueue builds the pending review queue for a reviewer. Drafts prepared
// by the viewer are excluded because the same person cannot finalize them.
// Ranking keys, in order, are risk level, metric value and waiting time
// (highest/longest first). The optional filter must be a PriorityLevel value.
func (s *priorityDecisionService) ReviewQueue(ctx context.Context, viewer, filter string) (PriorityReviewQueue, error) {
	filter = strings.TrimSpace(filter)
	if filter != "" && !constants.CanTransition(constants.PriorityDecisionTransitions, "draft", filter) {
		return PriorityReviewQueue{}, fmt.Errorf("%w: suggested level %q", ErrInvalidInput, filter)
	}
	drafts, err := s.repository.ListByStatus(ctx, model.PriorityDecisionInitialStatus)
	if err != nil {
		return PriorityReviewQueue{}, err
	}

	now := time.Now().UTC()
	queue := PriorityReviewQueue{Items: make([]PriorityReviewItem, 0), TotalDrafts: len(drafts), Filter: filter}
	for _, draft := range drafts {
		if draft.PreparedBy == viewer {
			queue.ExcludedOwnCount++
			continue
		}
		waitingHours := roundWaitingHours(now.Sub(draft.UpdatedAt.UTC()))
		riskRank := reviewRiskRank[draft.RiskLevel]
		suggested := suggestPriorityLevel(riskRank, draft.MetricValue, waitingHours)
		if filter != "" && suggested != filter {
			queue.FilteredCount++
			continue
		}
		queue.Items = append(queue.Items, PriorityReviewItem{
			ID: draft.ID, Code: draft.Code, Name: draft.Name, Facility: draft.Facility,
			Owner: draft.Owner, Category: draft.Category, RiskLevel: draft.RiskLevel, RiskRank: riskRank,
			MetricValue: draft.MetricValue, MetricUnit: draft.MetricUnit, RelatedCode: draft.RelatedCode,
			PreparedBy: draft.PreparedBy, Version: draft.Version, UpdatedAt: draft.UpdatedAt.UTC(),
			SuggestedLevel: suggested, WaitingHours: waitingHours,
			SortReason: buildSortReason(draft.RiskLevel, riskRank, draft.MetricValue, draft.MetricUnit, waitingHours, suggested),
		})
	}

	sort.SliceStable(queue.Items, func(i, j int) bool {
		left, right := queue.Items[i], queue.Items[j]
		if left.RiskRank != right.RiskRank {
			return left.RiskRank > right.RiskRank
		}
		if left.MetricValue != right.MetricValue {
			return left.MetricValue > right.MetricValue
		}
		if left.WaitingHours != right.WaitingHours {
			return left.WaitingHours > right.WaitingHours
		}
		return left.ID < right.ID
	})
	queue.QueueCount = len(queue.Items)
	for index := range queue.Items {
		queue.Items[index].Rank = index + 1
	}
	if queue.QueueCount == 0 {
		queue.EmptyReason = buildEmptyReason(queue)
	}
	return queue, nil
}

func suggestPriorityLevel(riskRank int, metricValue, waitingHours float64) string {
	switch {
	case riskRank >= 4:
		return string(constants.PriorityLevelUrgent)
	case riskRank >= 3 && (metricValue >= highMetricValue || waitingHours >= urgentWaitHours):
		return string(constants.PriorityLevelUrgent)
	case waitingHours >= urgentWaitHours:
		return string(constants.PriorityLevelRestrict)
	case riskRank >= 3:
		return string(constants.PriorityLevelRestrict)
	case riskRank >= 2 && (metricValue >= highMetricValue || waitingHours >= restrictWaitHours):
		return string(constants.PriorityLevelRestrict)
	default:
		return string(constants.PriorityLevelObserve)
	}
}

func buildSortReason(riskLevel string, riskRank int, metricValue float64, metricUnit string, waitingHours float64, suggested string) string {
	riskLabel, ok := reviewRiskLabels[riskLevel]
	if !ok {
		riskLabel = riskLevel
	}
	unit := strings.TrimSpace(metricUnit)
	if unit == "" {
		unit = "分"
	}
	return fmt.Sprintf(
		"风险等级%s（第%d档）、指标 %.1f %s、自最近更新已等待 %.1f 小时；按风险等级→指标值→等待时长降序排列，建议等级 %s",
		riskLabel, riskRank, metricValue, unit, waitingHours, suggested,
	)
}

func buildEmptyReason(queue PriorityReviewQueue) string {
	switch {
	case queue.TotalDrafts == 0:
		return "当前没有处于 draft 状态的优先级草稿，待复核队列为空。"
	case queue.ExcludedOwnCount == queue.TotalDrafts:
		return fmt.Sprintf("现有 %d 条草稿均由你本人拟制，按职责分离要求不计入你的待复核队列，需等待其他复核员处理。", queue.TotalDrafts)
	case queue.Filter != "":
		return fmt.Sprintf("队列中没有建议等级为 %s 的草稿（已排除本人拟制 %d 条），可切换或清除建议等级筛选。", queue.Filter, queue.ExcludedOwnCount)
	default:
		return "暂无可复核的他人草稿。"
	}
}

func roundWaitingHours(duration time.Duration) float64 {
	hours := duration.Hours()
	if hours < 0 {
		hours = 0
	}
	return math.Round(hours*10) / 10
}
