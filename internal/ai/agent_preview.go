package ai

type agentStepPreview struct {
	MessageID                string
	Step                     int
	Plan, Reasoning, Summary string
}

func (l *AgentLoop) applyPreview(run *AgentRun) {
	if run.State != "thinking" || len(run.Steps) == 0 {
		return
	}
	raw, ok := l.previews.Load(run.ID)
	if !ok {
		return
	}
	preview := raw.(agentStepPreview)
	step := &run.Steps[len(run.Steps)-1]
	if preview.Step != step.Number || preview.MessageID != step.MessageID {
		return
	}
	step.Plan = preview.Plan
	step.ReasoningContent = preview.Reasoning
	step.ReasoningSummary = preview.Summary
}
