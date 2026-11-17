package ai

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Stored context is data. Scope is rechecked before each model/tool boundary;
// historical scope revisions never silently grant access to revoked resources.
func executionEvidence(record sessionRecord, view SessionView) string {
	runs := []AgentRun{}
	for _, run := range record.AgentRuns {
		if run.View.ScopeRevision == record.Scope.Revision && agentTerminal(run.View.State) {
			visible := true
			for _, step := range run.View.Steps {
				for _, id := range step.NodeIDs {
					if !slices.Contains(view.AllowedNodeIDs, id) || !slices.Contains(view.actionNodes[step.ToolName], id) {
						visible = false
					}
				}
			}
			if visible {
				runs = append(runs, run.View)
			}
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})
	var out strings.Builder
	for _, run := range runs {
		fmt.Fprintf(&out, "\n[执行记录 %s 状态=%s] 目标：%s\n", run.ID, run.State, run.Goal)
		for _, step := range run.Steps {
			fmt.Fprintf(&out, "步骤 %d 工具=%s 节点=%s 状态=%s 任务=%s\n", step.Number, step.ToolName, strings.Join(step.NodeIDs, ","), step.State, step.TaskID)
			if step.Result != "" {
				fmt.Fprintf(&out, "回执：%s\n", step.Result)
			}
		}
		if run.FinalText != "" {
			fmt.Fprintf(&out, "历史答复（不是独立验收）：%s\n", run.FinalText)
		}
		if run.State == "unknown" {
			out.WriteString("结果未知：不得重放原副作用；须先核对实际状态。\n")
		}
	}
	return out.String()
}
func executionMemory(record sessionRecord, view SessionView) string {
	var out strings.Builder
	if record.ContextMemoryVersion == 2 && record.ContextSummary != "" {
		out.WriteString("[较早对话记忆]\n" + record.ContextSummary + "\n")
	}
	if record.Conversation != nil {
		for i, message := range record.Conversation.Messages {
			if record.ContextMemoryVersion == 2 && i < record.ContextCutoff || message.AgentRunID != "" || message.Role != "user" && !compactableMessage(message, record.Conversation.Runs[message.MessageID]) {
				continue
			}
			fmt.Fprintf(&out, "[%s %s]\n%s\n", message.Role, message.MessageID, message.RedactedContent)
		}
	}
	out.WriteString(executionEvidence(record, view))
	return out.String()
}

// Preserve user text exactly. Only adjacent repeated assistant lines are
// represented with a repetition count; identifiers and constraints are never sliced.
func compactRecord(role, text string) string {
	if role == "user" {
		return "[用户目标与约束（完整保留）]\n" + text
	}
	lines := strings.Split(text, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		out.WriteString(lines[i])
		if j-i > 1 {
			fmt.Fprintf(&out, " [连续相同行 %d 次]", j-i)
		}
		if j < len(lines) {
			out.WriteByte('\n')
		}
		i = j
	}
	return "[历史助手答复（未经独立验真）]\n" + out.String()
}
