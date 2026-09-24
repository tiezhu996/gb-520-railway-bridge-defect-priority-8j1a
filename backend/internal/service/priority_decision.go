package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
)

type PriorityDecisionService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.PriorityDecision], error)
	Get(context.Context, uint) (model.PriorityDecision, error)
	Create(context.Context, dto.CreatePriorityDecision, string, string) (model.PriorityDecision, error)
	Update(context.Context, uint, dto.UpdatePriorityDecision, string, string, string) (model.PriorityDecision, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.PriorityDecision, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
	ReviewQueue(context.Context, string, string) (dto.ReviewQueueResponse, error)
}

type priorityDecisionService struct {
	repository repository.PriorityDecisionRepository
	security   SecurityService
}

func NewPriorityDecisionService(repo repository.PriorityDecisionRepository, security SecurityService) PriorityDecisionService {
	return &priorityDecisionService{repository: repo, security: security}
}

func (s *priorityDecisionService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.PriorityDecision], error) {
	return s.repository.List(ctx, query)
}

func (s *priorityDecisionService) Get(ctx context.Context, id uint) (model.PriorityDecision, error) {
	return s.repository.Get(ctx, id)
}

func (s *priorityDecisionService) Create(ctx context.Context, input dto.CreatePriorityDecision, actor, requestID string) (model.PriorityDecision, error) {
	if err := validatePriorityDecisionBusinessFields(input.Code, input.Name, input.Facility, input.Owner, input.Evidence, input.RelatedCode); err != nil {
		return model.PriorityDecision{}, err
	}
	item := model.PriorityDecision{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.PriorityDecisionInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		PreparedBy:  actor,
	}
	revision, err := newPriorityRevision(item, "decision draft created", actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.CreateWithRevision(ctx, &item, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("create 优先级决定: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "PriorityDecision", item.ID, "", item.Status, "created 优先级决定")
	return s.repository.Get(ctx, item.ID)
}

func (s *priorityDecisionService) Update(ctx context.Context, id uint, input dto.UpdatePriorityDecision, actor, role, requestID string) (model.PriorityDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if current.Status != model.PriorityDecisionInitialStatus {
		return model.PriorityDecision{}, ErrDecisionLocked
	}
	if actor != current.PreparedBy && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrNotDecisionOwner
	}
	if err := validatePriorityDecisionBusinessFields(current.Code, input.Name, input.Facility, input.Owner, input.Evidence, input.RelatedCode); err != nil {
		return model.PriorityDecision{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, "draft business fields updated", actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("update 优先级决定: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "PriorityDecision", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *priorityDecisionService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.PriorityDecision, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if role != model.RoleReviewer && role != model.RoleAdmin {
		return model.PriorityDecision{}, ErrReviewRole
	}
	if actor == current.PreparedBy {
		return model.PriorityDecision{}, ErrSeparationOfDuty
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.PriorityDecisionTransitions, current.Status, target) {
		return model.PriorityDecision{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision, err := newPriorityRevision(current, strings.TrimSpace(input.Reason), actor, requestID)
	if err != nil {
		return model.PriorityDecision{}, err
	}
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, &revision); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("transition 优先级决定: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "PriorityDecision", id, before, target, input.Reason); err != nil {
		return model.PriorityDecision{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *priorityDecisionService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != model.PriorityDecisionInitialStatus {
		return ErrDecisionLocked
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "PriorityDecision", id, current.Status, "deleted", "soft deleted 优先级决定")
}

func (s *priorityDecisionService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

// ReviewQueue assembles the reviewer work queue: only drafts prepared by other
// users are eligible because a reviewer cannot finalize their own work. Items
// are ordered by risk rank, then metric value, then waiting time, all in the
// urgency-first direction. The response always explains why it is empty.
func (s *priorityDecisionService) ReviewQueue(ctx context.Context, actor, suggestedLevel string) (dto.ReviewQueueResponse, error) {
	suggestedLevel = strings.TrimSpace(suggestedLevel)
	if suggestedLevel != "" && !constants.IsPriorityLevel(suggestedLevel) {
		return dto.ReviewQueueResponse{}, fmt.Errorf("%w: suggestedLevel must be one of observe/restrict/urgent", ErrInvalidInput)
	}
	drafts, err := s.repository.ListDrafts(ctx)
	if err != nil {
		return dto.ReviewQueueResponse{}, err
	}
	now := time.Now().UTC()
	response := dto.ReviewQueueResponse{
		Items:          make([]dto.ReviewQueueItem, 0),
		TotalDrafts:    int64(len(drafts)),
		SuggestedLevel: suggestedLevel,
	}
	reviewable := make([]dto.ReviewQueueItem, 0, len(drafts))
	for _, draft := range drafts {
		if draft.PreparedBy == actor {
			response.ExcludedOwn++
			continue
		}
		queuedSince := draft.UpdatedAt.UTC()
		waitingHours := math.Max(0, now.Sub(queuedSince).Hours())
		suggestion := constants.SuggestedLevelForRisk(draft.RiskLevel)
		reviewable = append(reviewable, dto.ReviewQueueItem{
			ID:             draft.ID,
			Code:           draft.Code,
			Name:           draft.Name,
			Facility:       draft.Facility,
			Owner:          draft.Owner,
			Category:       draft.Category,
			RiskLevel:      draft.RiskLevel,
			RiskRank:       constants.RankRisk(draft.RiskLevel),
			MetricValue:    draft.MetricValue,
			MetricUnit:     draft.MetricUnit,
			PreparedBy:     draft.PreparedBy,
			Version:        draft.Version,
			SuggestedLevel: suggestion,
			QueuedSince:    queuedSince,
			WaitingHours:   math.Round(waitingHours*10) / 10,
		})
	}
	response.VisibleDrafts = len(reviewable)
	sort.SliceStable(reviewable, func(i, j int) bool {
		if reviewable[i].RiskRank != reviewable[j].RiskRank {
			return reviewable[i].RiskRank > reviewable[j].RiskRank
		}
		if reviewable[i].MetricValue != reviewable[j].MetricValue {
			return reviewable[i].MetricValue > reviewable[j].MetricValue
		}
		return reviewable[i].QueuedSince.Before(reviewable[j].QueuedSince)
	})
	for index := range reviewable {
		reviewable[index].Order = index + 1
		reviewable[index].OrderingReasons = queueOrderingReasons(reviewable[index], index, reviewable)
		if suggestedLevel == "" || reviewable[index].SuggestedLevel == suggestedLevel {
			response.Items = append(response.Items, reviewable[index])
		} else {
			response.FilteredOut++
		}
	}
	response.EmptyReason = buildEmptyQueueReason(response)
	return response, nil
}

var riskLevelLabels = map[string]string{
	"low": "低风险", "medium": "中风险", "high": "高风险", "critical": "严重风险",
}

func riskLevelText(level string) string {
	if label, ok := riskLevelLabels[level]; ok {
		return label
	}
	return "未知风险"
}

// queueOrderingReasons explains every tie-breaker that produced the position so
// reviewers understand why a draft is where it is.
func queueOrderingReasons(item dto.ReviewQueueItem, index int, ordered []dto.ReviewQueueItem) []string {
	reasons := []string{
		fmt.Sprintf("%s（风险序 %d）优先排序", riskLevelText(item.RiskLevel), item.RiskRank),
		fmt.Sprintf("指标值 %.1f %s 同风险内高者优先", item.MetricValue, strings.TrimSpace(item.MetricUnit)),
		fmt.Sprintf("已等待 %.1f 小时（自 %s），同风险同指标下等待久者优先", item.WaitingHours, item.QueuedSince.Format("01-02 15:04")),
	}
	if index > 0 && ordered[index-1].RiskRank > item.RiskRank {
		reasons = append(reasons, fmt.Sprintf("排在 %s 之后：其风险等级更高", ordered[index-1].Code))
	}
	if index+1 < len(ordered) {
		next := ordered[index+1]
		if next.RiskRank == item.RiskRank && next.MetricValue == item.MetricValue {
			reasons = append(reasons, fmt.Sprintf("排在 %s 之前：等待时间更长", next.Code))
		}
	}
	return reasons
}

func buildEmptyQueueReason(response dto.ReviewQueueResponse) string {
	if len(response.Items) > 0 {
		return ""
	}
	if response.TotalDrafts == 0 {
		return "当前没有处于 draft 状态、等待复核的优先级决定草稿。"
	}
	if response.VisibleDrafts == 0 {
		return fmt.Sprintf("共有 %d 条待复核草稿，但均由你本人拟制，按职责分离规则不进入你的队列。", response.TotalDrafts)
	}
	if response.SuggestedLevel != "" {
		return fmt.Sprintf("可复核草稿共 %d 条（另有 %d 条为你本人拟制已排除），但没有建议等级为 %s 的草稿。", response.VisibleDrafts, response.ExcludedOwn, response.SuggestedLevel)
	}
	return "没有可展示的待复核草稿。"
}

func validatePriorityDecisionBusinessFields(code, name, facility, owner, evidence, relatedCode string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" || strings.TrimSpace(evidence) == "" || strings.TrimSpace(relatedCode) == "" {
		return ErrInvalidInput
	}
	return nil
}

func newPriorityRevision(item model.PriorityDecision, reason, actor, requestID string) (model.PriorityDecisionRevision, error) {
	item.Revisions = nil
	snapshot, err := json.Marshal(item)
	if err != nil {
		return model.PriorityDecisionRevision{}, fmt.Errorf("serialize priority decision revision: %w", err)
	}
	return model.PriorityDecisionRevision{
		Version: item.Version, Status: item.Status, Evidence: item.Evidence,
		Reason: strings.TrimSpace(reason), Actor: actor, RequestID: requestID,
		Snapshot: string(snapshot), CreatedAt: time.Now().UTC(),
	}, nil
}
