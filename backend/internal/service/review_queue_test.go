package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/config"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/constants"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/model"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newReviewQueueTestService(t *testing.T) (PriorityDecisionService, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:review-queue-%d?mode=memory&cache=shared", time.Now().UnixNano())
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

func seedDraft(t *testing.T, db *gorm.DB, code, preparedBy, risk string, metric float64, age time.Duration) model.PriorityDecision {
	t.Helper()
	updated := time.Now().UTC().Add(-age)
	item := model.PriorityDecision{
		BaseModel: model.BaseModel{
			Code: code, Name: "待复核优先级决定", Status: model.PriorityDecisionInitialStatus,
			Version: 1, CreatedAt: updated, UpdatedAt: updated,
		},
		Facility: "K42 bridge", Owner: "disposal team", Category: "structural",
		RiskLevel: risk, MetricValue: metric, MetricUnit: "score",
		Evidence: "evidence", RelatedCode: "DF-Q", PreparedBy: preparedBy,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed draft %s: %v", code, err)
	}
	return item
}

func TestReviewQueueOrdersByRiskMetricAndWaitingTime(t *testing.T) {
	service, db := newReviewQueueTestService(t)
	ctx := context.Background()

	// Same risk, different metric: metric wins regardless of waiting time.
	a := seedDraft(t, db, "PD-Q-A", "operator-a", "high", 60, 2*time.Hour)
	b := seedDraft(t, db, "PD-Q-B", "operator-b", "high", 90, 1*time.Hour)
	// Lower risk can never overtake higher risk even with a large metric.
	c := seedDraft(t, db, "PD-Q-C", "operator-c", "low", 999, 48*time.Hour)
	// Same risk and metric: longer waiting time wins.
	d := seedDraft(t, db, "PD-Q-D", "operator-d", "critical", 50, 5*time.Hour)
	e := seedDraft(t, db, "PD-Q-E", "operator-e", "critical", 50, 20*time.Hour)
	// Reviewer's own draft must never enter the queue.
	seedDraft(t, db, "PD-Q-OWN", "reviewer", "critical", 100, 72*time.Hour)

	queue, err := service.ReviewQueue(ctx, "reviewer", "")
	if err != nil {
		t.Fatalf("review queue: %v", err)
	}
	if queue.TotalDrafts != 6 || queue.ExcludedOwn != 1 || queue.VisibleDrafts != 5 {
		t.Fatalf("unexpected queue counters: %+v", queue)
	}
	if queue.EmptyReason != "" {
		t.Fatalf("non-empty queue must not carry an empty reason, got %q", queue.EmptyReason)
	}
	gotCodes := make([]string, 0, len(queue.Items))
	for _, item := range queue.Items {
		gotCodes = append(gotCodes, item.Code)
	}
	wantCodes := []string{e.Code, d.Code, b.Code, a.Code, c.Code}
	for i := range wantCodes {
		if gotCodes[i] != wantCodes[i] {
			t.Fatalf("position %d: want %s, got order %v", i, wantCodes[i], gotCodes)
		}
	}
	if queue.Items[0].Order != 1 || queue.Items[0].SuggestedLevel != string(constants.PriorityLevelUrgent) {
		t.Fatalf("head item should be rank 1 with urgent suggestion: %+v", queue.Items[0])
	}
	if queue.Items[0].WaitingHours < 19 {
		t.Fatalf("waiting hours not computed from updatedAt: %+v", queue.Items[0])
	}
	if len(queue.Items[0].OrderingReasons) < 3 {
		t.Fatalf("ordering reasons must explain risk, metric and waiting: %+v", queue.Items[0].OrderingReasons)
	}
	if queue.Items[0].PreparedBy != "operator-e" {
		t.Fatalf("own draft leaked into queue head: %+v", queue.Items[0])
	}
	if bItem := queue.Items[2]; bItem.Code != "PD-Q-B" || bItem.SuggestedLevel != string(constants.PriorityLevelRestrict) {
		t.Fatalf("high risk should suggest restrict: %+v", bItem)
	}
}

func TestReviewQueueFiltersBySuggestedLevel(t *testing.T) {
	service, db := newReviewQueueTestService(t)
	ctx := context.Background()
	seedDraft(t, db, "PD-F-CRIT", "op-1", "critical", 80, 3*time.Hour)
	seedDraft(t, db, "PD-F-HIGH", "op-2", "high", 70, 4*time.Hour)
	seedDraft(t, db, "PD-F-LOW", "op-3", "low", 10, 6*time.Hour)

	queue, err := service.ReviewQueue(ctx, "reviewer", "restrict")
	if err != nil {
		t.Fatalf("filtered review queue: %v", err)
	}
	if len(queue.Items) != 1 || queue.Items[0].Code != "PD-F-HIGH" {
		t.Fatalf("restrict filter should return only the high risk draft: %+v", queue.Items)
	}
	if queue.FilteredOut != 2 {
		t.Fatalf("expected two drafts filtered out, got %d", queue.FilteredOut)
	}

	if _, err := service.ReviewQueue(ctx, "reviewer", "bogus"); err == nil {
		t.Fatal("invalid suggested level must be rejected")
	}
}

func TestReviewQueueEmptyReasons(t *testing.T) {
	ctx := context.Background()

	t.Run("no drafts at all", func(t *testing.T) {
		service, _ := newReviewQueueTestService(t)
		queue, err := service.ReviewQueue(ctx, "reviewer", "")
		if err != nil {
			t.Fatalf("empty queue: %v", err)
		}
		if len(queue.Items) != 0 || queue.EmptyReason == "" {
			t.Fatalf("expected explained empty queue: %+v", queue)
		}
	})

	t.Run("only own drafts", func(t *testing.T) {
		service, db := newReviewQueueTestService(t)
		seedDraft(t, db, "PD-OWN-1", "reviewer", "critical", 90, 2*time.Hour)
		queue, err := service.ReviewQueue(ctx, "reviewer", "")
		if err != nil {
			t.Fatalf("own-only queue: %v", err)
		}
		if len(queue.Items) != 0 || queue.ExcludedOwn != 1 || queue.VisibleDrafts != 0 {
			t.Fatalf("own drafts must be excluded: %+v", queue)
		}
		if queue.EmptyReason == "" {
			t.Fatal("queue must explain that all drafts are the caller's own")
		}
	})

	t.Run("filter matches nothing", func(t *testing.T) {
		service, db := newReviewQueueTestService(t)
		seedDraft(t, db, "PD-OTHER", "operator", "low", 5, time.Hour)
		queue, err := service.ReviewQueue(ctx, "reviewer", "urgent")
		if err != nil {
			t.Fatalf("filtered empty queue: %v", err)
		}
		if len(queue.Items) != 0 || queue.EmptyReason == "" {
			t.Fatalf("filter-empty queue must explain itself: %+v", queue)
		}
	})
}
