package quota_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

func allowAll(context.Context, auth.Check) error { return nil }

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeNotifier struct {
	mu     sync.Mutex
	alerts []notifications.Alert
	err    error
}

func (f *fakeNotifier) SendAlert(_ context.Context, alert notifications.Alert) (notifications.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, alert)
	return notifications.Delivery{ID: int64(len(f.alerts)), Status: notifications.DeliveryProviderAccepted}, f.err
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.alerts)
}

func (f *fakeNotifier) at(index int) notifications.Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alerts[index]
}

var (
	periodStart = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	periodEnd   = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

type fixture struct {
	db       *storage.DB
	nodes    *nodes.Repo
	repo     *quota.Repo
	audit    *audit.SQLRecorder
	service  *quota.Service
	clock    *testClock
	notifier *fakeNotifier
}

func buildFixture(t *testing.T, db *storage.DB, clock *testClock, authorizer auth.Authorizer, notifier quota.Notifier) fixture {
	t.Helper()
	nodeRepo, err := nodes.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := quota.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(quota.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(allowAll)
	}
	service, err := quota.NewService(quota.Options{
		DB: db, Repo: repo, Nodes: nodeRepo, Audit: recorder,
		Authorizer: authorizer, Notifier: notifier, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	fake, _ := notifier.(*fakeNotifier)
	return fixture{db: db, nodes: nodeRepo, repo: repo, audit: recorder, service: service, clock: clock, notifier: fake}
}

func newFixture(t *testing.T, authorizer auth.Authorizer, notifier quota.Notifier) fixture {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	db := testkit.NewDB(t, nodes.Migrations(), quota.Migrations(), audit.Migrations())
	h := buildFixture(t, db, clock, authorizer, notifier)
	for _, id := range []string{"n1", "n2"} {
		if _, err := h.nodes.Create(context.Background(), db, nodes.NodeCreateInput{
			ID: id, Name: id, Status: nodes.StatusOnline,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (f fixture) upsertPolicy(t *testing.T, nodeID string, limit int64, warning int, expiry time.Time) quota.Policy {
	t.Helper()
	policy, err := f.service.UpsertPolicy(context.Background(), "admin", "req-policy-"+nodeID, quota.PolicyInput{
		NodeID: nodeID, MonthlyLimitBytes: limit, WarningPercent: warning, ExpiryAt: expiry,
	})
	if err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}
	return policy
}

func (f fixture) record(t *testing.T, nodeID string, in, out float64, source quota.Source) quota.Usage {
	t.Helper()
	usage, err := f.service.RecordUsage(context.Background(), quota.UsageSample{
		NodeID: nodeID, PeriodStart: periodStart, PeriodEnd: periodEnd,
		BytesIn: in, BytesOut: out, Source: source, ObservedAt: f.clock.Now(),
	})
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	return usage
}

func TestMissingUsageIsNotZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	usage, found, err := h.service.GetUsage(ctx, "admin", "n1", periodStart)
	if err != nil || found {
		t.Fatalf("missing usage found=%t usage=%+v err=%v", found, usage, err)
	}
	evaluation, err := h.service.Evaluate(ctx, "admin", "n1", periodStart, h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Usage != nil || evaluation.AlertRecommended || evaluation.Level != quota.LevelOK {
		t.Fatalf("missing usage evaluation = %+v", evaluation)
	}
	reminders, err := h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 0 || h.notifier.count() != 0 {
		t.Fatalf("missing usage reminders = %+v notifications=%d err=%v", reminders, h.notifier.count(), err)
	}
	if _, err := h.service.Evaluate(ctx, "admin", "n2", periodStart, h.clock.Now()); !errors.Is(err, quota.ErrPolicyNotFound) {
		t.Fatalf("evaluate without policy err = %v", err)
	}
}

func TestNoAutoBlockOnExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 700, 500, quota.SourceAgent)
	evaluation, err := h.service.Evaluate(ctx, "admin", "n1", periodStart, h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Level != quota.LevelExceeded || !evaluation.AlertRecommended || evaluation.PercentUsed != 120 {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	node, err := h.nodes.Get(ctx, "n1")
	if err != nil || node.Status != nodes.StatusOnline {
		t.Fatalf("quota evaluation changed node status: %+v, %v", node, err)
	}
	if _, err := h.service.RunReminders(ctx, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	node, err = h.nodes.Get(ctx, "n1")
	if err != nil || node.Status != nodes.StatusOnline {
		t.Fatalf("exceeded reminder changed node status: %+v, %v", node, err)
	}
	if h.notifier.count() != 1 || !strings.Contains(h.notifier.at(0).DedupeKey, "exceeded") {
		t.Fatalf("exceeded reminders = %d %+v", h.notifier.count(), h.notifier.at(0))
	}
}

func TestWarningAndExceededThresholds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 400, 300, quota.SourceAgent)
	evaluation, err := h.service.Evaluate(ctx, "admin", "n1", periodStart, h.clock.Now())
	if err != nil || evaluation.Level != quota.LevelOK || evaluation.PercentUsed != 70 {
		t.Fatalf("below warning = %+v, %v", evaluation, err)
	}
	h.clock.Advance(time.Second)
	h.record(t, "n1", 450, 350, quota.SourceAgent)
	evaluation, err = h.service.Evaluate(ctx, "admin", "n1", periodStart, h.clock.Now())
	if err != nil || evaluation.Level != quota.LevelWarning || evaluation.PercentUsed != 80 {
		t.Fatalf("warning = %+v, %v", evaluation, err)
	}
	h.clock.Advance(time.Second)
	h.record(t, "n1", 600, 400, quota.SourceAgent)
	evaluation, err = h.service.Evaluate(ctx, "admin", "n1", periodStart, h.clock.Now())
	if err != nil || evaluation.Level != quota.LevelExceeded || evaluation.PercentUsed != 100 {
		t.Fatalf("exceeded = %+v, %v", evaluation, err)
	}
}

func TestWarningReminderDedupeAndEscalation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 450, 400, quota.SourceAgent)
	reminders, err := h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 1 || reminders[0].Threshold != quota.LevelWarning {
		t.Fatalf("warning reminders = %+v, %v", reminders, err)
	}
	reminders, err = h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 0 || h.notifier.count() != 1 {
		t.Fatalf("warning dedupe reminders=%+v notifications=%d err=%v", reminders, h.notifier.count(), err)
	}
	h.clock.Advance(time.Minute)
	h.record(t, "n1", 600, 500, quota.SourceAgent)
	reminders, err = h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 1 || reminders[0].Threshold != quota.LevelExceeded {
		t.Fatalf("exceeded reminders = %+v, %v", reminders, err)
	}
	if h.notifier.count() != 2 {
		t.Fatalf("notification count = %d", h.notifier.count())
	}
	if h.notifier.at(0).DedupeKey == h.notifier.at(1).DedupeKey {
		t.Fatal("warning and exceeded reused the same dedupe key")
	}
}

func TestExpiryReminderDedupe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1_000_000_000, 80, h.clock.Now().Add(5*24*time.Hour))
	h.record(t, "n1", 100, 100, quota.SourceProvider)
	reminders, err := h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 1 || reminders[0].Threshold != quota.LevelExpiring {
		t.Fatalf("expiry reminders = %+v, %v", reminders, err)
	}
	if !strings.Contains(reminders[0].DedupeKey, "expiring") || h.notifier.count() != 1 {
		t.Fatalf("expiry reminder = %+v notifications=%d", reminders[0], h.notifier.count())
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: quota.ActionReminder})
	if err != nil || page.Total != 1 {
		t.Fatalf("reminder audit = %+v, %v", page, err)
	}
	reminders, err = h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 0 || h.notifier.count() != 1 {
		t.Fatalf("expiry dedupe reminders=%+v notifications=%d err=%v", reminders, h.notifier.count(), err)
	}
	page, err = h.audit.Query(ctx, audit.Query{Action: quota.ActionReminder})
	if err != nil || page.Total != 1 {
		t.Fatalf("deduped reminder audit = %+v, %v", page, err)
	}
	h.clock.Advance(24 * time.Hour)
	reminders, err = h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 0 || h.notifier.count() != 1 {
		t.Fatalf("later expiry dedupe reminders=%+v notifications=%d err=%v", reminders, h.notifier.count(), err)
	}
}

func TestExpiryReminderWithoutUsageDoesNotFabricateZero(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, h.clock.Now().Add(2*24*time.Hour))
	reminders, err := h.service.RunReminders(ctx, h.clock.Now())
	if err != nil || len(reminders) != 1 || h.notifier.count() != 1 {
		t.Fatalf("no-data expiry reminders = %+v notifications=%d err=%v", reminders, h.notifier.count(), err)
	}
	if !strings.Contains(h.notifier.at(0).Body, "no current traffic measurement") || strings.Contains(h.notifier.at(0).Body, "used 0") {
		t.Fatal("expiry reminder invented zero usage")
	}
}

func TestNegativeNaNInfAndInvalidPeriodRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	cases := []struct {
		name   string
		sample quota.UsageSample
		want   error
	}{
		{"negative in", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, BytesIn: -1, Source: quota.SourceAgent}, quota.ErrInvalidUsage},
		{"negative out", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, BytesOut: -1, Source: quota.SourceAgent}, quota.ErrInvalidUsage},
		{"nan", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, BytesIn: math.NaN(), Source: quota.SourceAgent}, quota.ErrInvalidUsage},
		{"inf", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, BytesOut: math.Inf(1), Source: quota.SourceAgent}, quota.ErrInvalidUsage},
		{"bad source", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, Source: "guessed"}, quota.ErrInvalidUsage},
		{"zero start", quota.UsageSample{NodeID: "n1", PeriodEnd: periodEnd, Source: quota.SourceAgent}, quota.ErrInvalidPeriod},
		{"end before start", quota.UsageSample{NodeID: "n1", PeriodStart: periodEnd, PeriodEnd: periodStart, Source: quota.SourceAgent}, quota.ErrInvalidPeriod},
		{"future observed", quota.UsageSample{NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd, Source: quota.SourceAgent, ObservedAt: h.clock.Now().Add(time.Hour)}, quota.ErrInvalidUsage},
		{"unknown node", quota.UsageSample{NodeID: "missing", PeriodStart: periodStart, PeriodEnd: periodEnd, Source: quota.SourceAgent}, quota.ErrUnknownNode},
	}
	for _, tc := range cases {
		if _, err := h.service.RecordUsage(ctx, tc.sample); !errors.Is(err, tc.want) {
			t.Fatalf("%s err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, found, err := h.service.GetUsage(ctx, "admin", "n1", periodStart); err != nil || found {
		t.Fatalf("rejected samples persisted: found=%t err=%v", found, err)
	}
	if _, err := h.service.UpsertPolicy(ctx, "admin", "req-missing", quota.PolicyInput{NodeID: "missing", MonthlyLimitBytes: 100, WarningPercent: 80}); !errors.Is(err, quota.ErrUnknownNode) {
		t.Fatalf("unknown node policy err = %v", err)
	}
}

func TestMonotonicAndCorrection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.record(t, "n1", 100, 100, quota.SourceAgent)
	h.clock.Advance(time.Second)
	if _, err := h.service.RecordUsage(ctx, quota.UsageSample{
		NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd,
		BytesIn: 50, BytesOut: 50, Source: quota.SourceAgent, ObservedAt: h.clock.Now(),
	}); !errors.Is(err, quota.ErrNonMonotonic) {
		t.Fatalf("decrease err = %v", err)
	}
	h.clock.Advance(time.Second)
	corrected, err := h.service.RecordUsage(ctx, quota.UsageSample{
		NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd,
		BytesIn: 50, BytesOut: 50, Source: quota.SourceCorrection, ObservedAt: h.clock.Now(),
	})
	if err != nil || corrected.TotalBytes() != 100 || corrected.Source != quota.SourceCorrection {
		t.Fatalf("correction = %+v, %v", corrected, err)
	}
	h.clock.Advance(time.Second)
	raised, err := h.service.RecordUsage(ctx, quota.UsageSample{
		NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd,
		BytesIn: 60, BytesOut: 60, Source: quota.SourceAgent, ObservedAt: h.clock.Now(),
	})
	if err != nil || raised.TotalBytes() != 120 {
		t.Fatalf("raise after correction = %+v, %v", raised, err)
	}
	h.clock.Advance(time.Second)
	billing, err := h.service.RecordUsage(ctx, quota.UsageSample{
		NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodEnd,
		BytesIn: 20, BytesOut: 20, Source: quota.SourceProvider, ObservedAt: h.clock.Now(),
	})
	if err != nil || billing.TotalBytes() != 40 || billing.Source != quota.SourceProvider {
		t.Fatalf("authoritative billing correction = %+v, %v", billing, err)
	}
}

func TestEstimateExpiryRequiresEnoughSamples(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	if _, found, err := h.service.EstimateExpiry(ctx, "admin", "n1", h.clock.Now()); err != nil || found {
		t.Fatalf("no sample estimate found=%t err=%v", found, err)
	}
	h.record(t, "n1", 100, 0, quota.SourceAgent)
	if _, found, err := h.service.EstimateExpiry(ctx, "admin", "n1", h.clock.Now()); err != nil || found {
		t.Fatalf("one sample estimate found=%t err=%v", found, err)
	}
	h.clock.Advance(2 * time.Hour)
	h.record(t, "n1", 300, 0, quota.SourceAgent)
	estimate, found, err := h.service.EstimateExpiry(ctx, "admin", "n1", h.clock.Now())
	if err != nil || !found {
		t.Fatalf("estimate found=%t err=%v", found, err)
	}
	if estimate.Samples != 2 || estimate.BytesPerDay < 2399 || estimate.BytesPerDay > 2401 {
		t.Fatalf("estimate = %+v", estimate)
	}
	wantExhausted := h.clock.Now().Add(7 * time.Hour)
	if estimate.ExhaustedAt.Before(wantExhausted.Add(-time.Minute)) || estimate.ExhaustedAt.After(wantExhausted.Add(time.Minute)) {
		t.Fatalf("exhausted at = %s, want ~%s", estimate.ExhaustedAt, wantExhausted)
	}
}

func TestPersistenceAndRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fleet.db")
	clock := &testClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	db := testkit.NewDBAt(t, path, nodes.Migrations(), quota.Migrations(), audit.Migrations())
	h := buildFixture(t, db, clock, nil, &fakeNotifier{})
	if _, err := h.nodes.Create(ctx, db, nodes.NodeCreateInput{ID: "n1", Name: "n1", Status: nodes.StatusOnline}); err != nil {
		t.Fatal(err)
	}
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 450, 400, quota.SourceAgent)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	h2 := buildFixture(t, reopened, clock, nil, &fakeNotifier{})
	usage, found, err := h2.service.GetUsage(ctx, "admin", "n1", periodStart)
	if err != nil || !found || usage.TotalBytes() != 850 {
		t.Fatalf("restarted usage found=%t usage=%+v err=%v", found, usage, err)
	}
	policy, found, err := h2.service.GetPolicy(ctx, "admin", "n1")
	if err != nil || !found || policy.MonthlyLimitBytes != 1000 {
		t.Fatalf("restarted policy found=%t policy=%+v err=%v", found, policy, err)
	}
	evaluation, err := h2.service.Evaluate(ctx, "admin", "n1", periodStart, clock.Now())
	if err != nil || evaluation.Level != quota.LevelWarning {
		t.Fatalf("restarted evaluation = %+v, %v", evaluation, err)
	}
}

func TestAuthorizationDenialAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied }), &fakeNotifier{})
	_, err := h.service.UpsertPolicy(ctx, "mallory", "req-deny", quota.PolicyInput{
		NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 80,
	})
	if !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v, want denied", err)
	}
	if _, err := h.repo.GetPolicy(ctx, "n1"); !errors.Is(err, quota.ErrPolicyNotFound) {
		t.Fatalf("denied write mutated storage: %v", err)
	}
	page, err := h.audit.Query(ctx, audit.Query{Action: quota.ActionWrite, Result: "denied"})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "mallory" {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
	if len(page.Items[0].NodeIDs) != 1 || page.Items[0].NodeIDs[0] != "n1" {
		t.Fatalf("denial node snapshot = %+v", page.Items[0].NodeIDs)
	}
}

func TestInvalidPolicyAndReminderReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	for _, input := range []quota.PolicyInput{
		{NodeID: "n1", MonthlyLimitBytes: 0, WarningPercent: 80},
		{NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 0},
		{NodeID: "n1", MonthlyLimitBytes: 1000, WarningPercent: 101},
		{NodeID: "", MonthlyLimitBytes: 1000, WarningPercent: 80},
	} {
		if _, err := h.service.UpsertPolicy(ctx, "admin", "req-bad", input); !errors.Is(err, quota.ErrInvalidPolicy) {
			t.Fatalf("policy %+v err = %v", input, err)
		}
	}
	if _, found, err := h.service.GetPolicy(ctx, "admin", "n1"); err != nil || found {
		t.Fatalf("missing policy found=%t err=%v", found, err)
	}
	if _, _, err := h.service.GetUsage(ctx, "admin", "n1", time.Time{}); !errors.Is(err, quota.ErrInvalidPeriod) {
		t.Fatalf("zero period err = %v", err)
	}
}

func TestRunRemindersFailsClosedWithoutNotifier(t *testing.T) {
	t.Parallel()
	h := newFixture(t, nil, nil)
	if _, err := h.service.RunReminders(context.Background(), h.clock.Now()); !errors.Is(err, quota.ErrNotifierUnavailable) {
		t.Fatalf("nil notifier err = %v", err)
	}
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.record(t, "n1", 900, 200, quota.SourceAgent)
	if _, err := h.service.RunReminders(context.Background(), h.clock.Now()); !errors.Is(err, quota.ErrNotifierUnavailable) {
		t.Fatalf("nil notifier with usage err = %v", err)
	}
	if _, err := h.service.RunReminders(context.Background(), time.Time{}); !errors.Is(err, quota.ErrNotifierUnavailable) {
		t.Fatalf("zero time err = %v", err)
	}
}

func TestListPoliciesFiltersDeniedNodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newFixture(t, nil, &fakeNotifier{})
	h.upsertPolicy(t, "n1", 1000, 80, time.Time{})
	h.upsertPolicy(t, "n2", 2000, 80, time.Time{})
	service, err := quota.NewService(quota.Options{
		DB: h.db, Repo: h.repo, Nodes: h.nodes, Audit: h.audit,
		Authorizer: authorizerFunc(func(_ context.Context, check auth.Check) error {
			if check.Action == auth.NodeReadAction && len(check.NodeIDs) == 1 && check.NodeIDs[0] == "n2" {
				return auth.ErrDenied
			}
			return nil
		}),
		Notifier: &fakeNotifier{}, Now: h.clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	policies, err := service.ListPolicies(ctx, "reader")
	if err != nil || len(policies) != 1 || policies[0].NodeID != "n1" {
		t.Fatalf("filtered policies = %+v, %v", policies, err)
	}
	if _, err := service.Evaluate(ctx, "reader", "n2", periodStart, h.clock.Now()); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied evaluate err = %v", err)
	}
}
