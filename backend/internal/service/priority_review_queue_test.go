package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/config"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/dto"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestPriorityReviewQueueRankingAndExclusions(t *testing.T) {
	svc, db := newPriorityReviewTestService(t)
	ctx := context.Background()

	// A: critical + highest metric must lead regardless of waiting time.
	a := mustCreateDraft(t, ctx, svc, "PD-QUEUE-A", "operator", "critical", 95)
	// B: high risk with high metric.
	b := mustCreateDraft(t, ctx, svc, "PD-QUEUE-B", "operator", "high", 80)
	// G and C share risk and metric; the longer-waiting G must rank first.
	g := mustCreateDraft(t, ctx, svc, "PD-QUEUE-G", "operator", "high", 30)
	c := mustCreateDraft(t, ctx, svc, "PD-QUEUE-C", "operator", "high", 30)
	// D: medium risk but very old draft, escalated by waiting time.
	d := mustCreateDraft(t, ctx, svc, "PD-QUEUE-D", "operator", "medium", 20)
	// E: drafted by the reviewer themselves, must be excluded.
	e := mustCreateDraft(t, ctx, svc, "PD-QUEUE-E", "reviewer", "critical", 99)
	// F: already finalized, never belongs to the draft queue.
	f := mustCreateDraft(t, ctx, svc, "PD-QUEUE-F", "operator", "critical", 90)

	setDraftAge(t, db, a.ID, 0)
	setDraftAge(t, db, b.ID, 10*time.Hour)
	setDraftAge(t, db, g.ID, 50*time.Hour)
	setDraftAge(t, db, c.ID, 5*time.Hour)
	setDraftAge(t, db, d.ID, 100*time.Hour)
	setDraftAge(t, db, e.ID, 200*time.Hour)
	if _, err := svc.Transition(ctx, f.ID, dto.TransitionRequest{
		Status: "urgent", ExpectedVersion: 1, Reason: "finalized outside queue",
	}, "admin", model.RoleAdmin, "req-final"); err != nil {
		t.Fatalf("finalize f: %v", err)
	}

	queue, err := svc.ReviewQueue(ctx, "reviewer", "")
	if err != nil {
		t.Fatalf("build review queue: %v", err)
	}

	wantCodes := []string{"PD-QUEUE-A", "PD-QUEUE-B", "PD-QUEUE-G", "PD-QUEUE-C", "PD-QUEUE-D"}
	if len(queue.Items) != len(wantCodes) {
		t.Fatalf("queue size = %d, want %d (queue=%+v)", len(queue.Items), len(wantCodes), queue.Items)
	}
	for index, want := range wantCodes {
		if queue.Items[index].Code != want {
			t.Fatalf("position %d = %s, want %s; full order %+v", index+1, queue.Items[index].Code, want, queueCodes(queue.Items))
		}
		if queue.Items[index].Rank != index+1 {
			t.Fatalf("%s rank = %d, want %d", want, queue.Items[index].Rank, index+1)
		}
	}
	if queue.TotalDrafts != 6 || queue.ExcludedOwnCount != 1 || queue.QueueCount != 5 {
		t.Fatalf("unexpected queue counters: %+v", queue)
	}
	for _, item := range queue.Items {
		if item.PreparedBy == "reviewer" {
			t.Fatalf("own draft %s leaked into reviewer queue", item.Code)
		}
	}

	assertSuggestion := func(code, want string) {
		t.Helper()
		for _, item := range queue.Items {
			if item.Code == code && item.SuggestedLevel != want {
				t.Fatalf("%s suggested = %s, want %s", code, item.SuggestedLevel, want)
			}
		}
	}
	assertSuggestion("PD-QUEUE-A", string(constants.PriorityLevelUrgent))
	assertSuggestion("PD-QUEUE-B", string(constants.PriorityLevelUrgent))
	assertSuggestion("PD-QUEUE-G", string(constants.PriorityLevelRestrict))
	assertSuggestion("PD-QUEUE-C", string(constants.PriorityLevelRestrict))
	assertSuggestion("PD-QUEUE-D", string(constants.PriorityLevelRestrict))

	gItem := queue.Items[2]
	if gItem.WaitingHours < 49 || gItem.WaitingHours > 51 {
		t.Fatalf("waiting hours for G = %.1f, want ~50", gItem.WaitingHours)
	}
	if gItem.SortReason == "" || gItem.SuggestedLevel == "" {
		t.Fatalf("rank reason and suggestion must be populated: %+v", gItem)
	}

	// Filtering by suggested level keeps only urgent suggestions.
	urgentQueue, err := svc.ReviewQueue(ctx, "reviewer", "urgent")
	if err != nil {
		t.Fatalf("filtered queue: %v", err)
	}
	if urgentQueue.Filter != "urgent" || len(urgentQueue.Items) != 2 ||
		urgentQueue.Items[0].Code != "PD-QUEUE-A" || urgentQueue.Items[1].Code != "PD-QUEUE-B" {
		t.Fatalf("urgent filter result = %+v", urgentQueue)
	}
	if urgentQueue.FilteredCount != 3 || urgentQueue.QueueCount != 2 {
		t.Fatalf("filtered counters = %+v", urgentQueue)
	}

	// Invalid suggested level is rejected as business input.
	if _, err := svc.ReviewQueue(ctx, "reviewer", "bogus"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid filter should fail with ErrInvalidInput, got %v", err)
	}

	// From the operator's view all operator drafts drop out; only reviewer's own
	// draft E is reviewable by someone else.
	operatorQueue, err := svc.ReviewQueue(ctx, "operator", "")
	if err != nil {
		t.Fatalf("operator queue: %v", err)
	}
	if len(operatorQueue.Items) != 1 || operatorQueue.Items[0].Code != "PD-QUEUE-E" || operatorQueue.ExcludedOwnCount != 5 {
		t.Fatalf("operator queue = %+v", operatorQueue)
	}
}

func TestPriorityReviewQueueEmptyReasons(t *testing.T) {
	ctx := context.Background()

	// No drafts at all.
	emptySvc, _ := newPriorityReviewTestService(t)
	emptyQueue, err := emptySvc.ReviewQueue(ctx, "reviewer", "")
	if err != nil {
		t.Fatalf("empty queue: %v", err)
	}
	if len(emptyQueue.Items) != 0 || emptyQueue.EmptyReason == "" {
		t.Fatalf("expected explained empty queue, got %+v", emptyQueue)
	}

	// Only the viewer's own drafts: queue empty with a separation-of-duties reason.
	ownSvc, _ := newPriorityReviewTestService(t)
	mustCreateDraft(t, ctx, ownSvc, "PD-OWN-1", "reviewer", "high", 70)
	mustCreateDraft(t, ctx, ownSvc, "PD-OWN-2", "reviewer", "low", 10)
	ownQueue, err := ownSvc.ReviewQueue(ctx, "reviewer", "")
	if err != nil {
		t.Fatalf("own-only queue: %v", err)
	}
	if len(ownQueue.Items) != 0 || ownQueue.ExcludedOwnCount != 2 || ownQueue.EmptyReason == "" {
		t.Fatalf("expected own-draft empty reason, got %+v", ownQueue)
	}

	// Filter that matches nothing must explain the active filter.
	mustCreateDraft(t, ctx, ownSvc, "PD-OTHER", "operator", "low", 5)
	filtered, err := ownSvc.ReviewQueue(ctx, "reviewer", "urgent")
	if err != nil {
		t.Fatalf("filtered own queue: %v", err)
	}
	if len(filtered.Items) != 0 || filtered.Filter != "urgent" || filtered.EmptyReason == "" {
		t.Fatalf("expected filter empty reason, got %+v", filtered)
	}
}

func newPriorityReviewTestService(t *testing.T) (PriorityDecisionService, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:review-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.PriorityDecision{}, &model.PriorityDecisionRevision{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	return NewPriorityDecisionService(repository.NewPriorityDecisionRepository(db), security), db
}

func mustCreateDraft(t *testing.T, ctx context.Context, svc PriorityDecisionService, code, preparedBy, risk string, metric float64) model.PriorityDecision {
	t.Helper()
	input := priorityCreateInput(code, "queue evidence")
	input.RiskLevel = risk
	input.MetricValue = metric
	created, err := svc.Create(ctx, input, preparedBy, "req-"+code)
	if err != nil {
		t.Fatalf("create %s: %v", code, err)
	}
	return created
}

func setDraftAge(t *testing.T, db *gorm.DB, id uint, age time.Duration) {
	t.Helper()
	if err := db.Exec("UPDATE priority_decisions SET updated_at = ? WHERE id = ?", time.Now().UTC().Add(-age), id).Error; err != nil {
		t.Fatalf("age draft %d: %v", id, err)
	}
}

func queueCodes(items []PriorityReviewItem) []string {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.Code)
	}
	return codes
}
