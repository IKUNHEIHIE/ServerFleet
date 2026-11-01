package updates

import (
	"context"
	"errors"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/tasks"
)

var ErrUpdateJobNotFound = errors.New("updates: task job not found")
var ErrUpdateJobDenied = errors.New("updates: task job denied")

// The list is an explicitly bounded recent unified-task window, not invented
// update history. Older jobs remain available by their exact task ID.
type JobPage struct {
	Items     []ApplyResult `json:"items"`
	Truncated bool          `json:"truncated"`
}
type TaskJobProjection struct {
	repo *tasks.Repo
	gate func(context.Context, Principal, tasks.Task) error
}

func NewTaskJobProjection(repo *tasks.Repo, gate func(context.Context, Principal, tasks.Task) error) (*TaskJobProjection, error) {
	if repo == nil || gate == nil {
		return nil, ErrArtifactRejected
	}
	return &TaskJobProjection{repo, gate}, nil
}
func (p *TaskJobProjection) view(ctx context.Context, principal Principal, t tasks.Task) (ApplyResult, error) {
	if !principal.Administrator || principal.SubjectID != t.SubmitterID || t.ActionID != ActionInstall {
		return ApplyResult{}, ErrUpdateJobNotFound
	}
	if err := p.gate(ctx, principal, t); err != nil {
		if err == auth.ErrDenied {
			return ApplyResult{}, ErrUpdateJobDenied
		}
		return ApplyResult{}, err
	}
	plan, err := frozenTaskPlan(t)
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{ID: t.ID, TaskID: t.ID, Status: string(t.Status), TargetVersion: plan.Attempted.Version}, nil
}
func (p *TaskJobProjection) Job(ctx context.Context, principal Principal, id string) (ApplyResult, error) {
	if !installID(id) {
		return ApplyResult{}, ErrUpdateJobNotFound
	}
	t, err := p.repo.Get(ctx, id)
	if errors.Is(err, tasks.ErrTaskNotFound) {
		return ApplyResult{}, ErrUpdateJobNotFound
	}
	if err != nil {
		return ApplyResult{}, err
	}
	return p.view(ctx, principal, t)
}
func (p *TaskJobProjection) Jobs(ctx context.Context, principal Principal) (JobPage, error) {
	if !principal.Administrator || !installID(principal.SubjectID) {
		return JobPage{}, ErrUpdateJobDenied
	}
	page := JobPage{Items: []ApplyResult{}}
	rows, err := p.repo.List(ctx, 200, 0)
	if err != nil {
		return JobPage{}, err
	}
	page.Truncated = len(rows) == 200
	for _, task := range rows {
		if task.ActionID != ActionInstall || task.SubmitterID != principal.SubjectID {
			continue
		}
		item, e := p.view(ctx, principal, task)
		if e != nil {
			return JobPage{}, e
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}
