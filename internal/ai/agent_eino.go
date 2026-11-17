package ai

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Eino owns the model/tool dispatch turn. A tool yields its proposal to the
// durable controller run; the next turn receives the real task observation.
// This avoids replacing the existing wait/resume ledger with a second store.
func (l *AgentLoop) generateWithEino(ctx context.Context, session, message string, prompt *agentPrompt) (Message, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var response Message
	var decision agentDecision
	generated, dispatched := 0, 0
	bridge := &einoTurnModel{generate: func(ctx context.Context) (*schema.Message, error) {
		generated++
		if generated != 1 {
			return nil, ErrGenerationUnknown
		}
		var err error
		response, err = l.conversation.Generate(ctx, session, message)
		if err != nil {
			return nil, err
		}
		if decodeAgentJSON([]byte(response.RedactedContent), &decision) != nil {
			return nil, ErrGenerationUnknown
		}
		output := &schema.Message{Role: schema.Assistant, Content: decision.Text}
		for _, item := range decision.Items {
			var native struct {
				Type, Name, Arguments string
				CallID                string `json:"call_id"`
			}
			if json.Unmarshal(item, &native) != nil {
				return nil, ErrGenerationUnknown
			}
			if native.Type == "function_call" {
				output.ToolCalls = append(output.ToolCalls, schema.ToolCall{ID: native.CallID, Type: "function", Function: schema.FunctionCall{Name: native.Name, Arguments: native.Arguments}})
			}
		}
		// Internal adapters/tests may provide the typed proposal directly.
		if len(output.ToolCalls) == 0 && decision.Call != nil {
			for _, t := range prompt.Tools {
				if t.Action == decision.Call.ToolName {
					args := map[string]any{"nodeIds": decision.Call.NodeIDs}
					var parameters map[string]string
					if json.Unmarshal(decision.Call.Parameters, &parameters) != nil {
						return nil, ErrGenerationUnknown
					}
					for key, value := range parameters {
						args[key] = value
					}
					raw, _ := json.Marshal(args)
					output.ToolCalls = append(output.ToolCalls, schema.ToolCall{ID: decision.Call.CallID, Type: "function", Function: schema.FunctionCall{Name: t.Name, Arguments: string(raw)}})
				}
			}
		}
		return output, nil
	}}
	tools := make([]tool.BaseTool, 0, len(prompt.Tools))
	direct := map[string]bool{}
	for _, definition := range prompt.Tools {
		tools = append(tools, &einoProposalTool{definition: definition, onDispatch: func() { dispatched++ }})
		direct[definition.Name] = true
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "serverfleet", Description: "Server Fleet operational planning", Instruction: agentInstructions, Model: bridge, MaxIterations: 1,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true}, ReturnDirectly: direct},
	})
	if err != nil {
		return Message{}, err
	}
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Query(ctx, prompt.Goal)
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return Message{}, event.Err
		}
	}
	expected := len(decision.Calls)
	if expected == 0 && decision.Call != nil {
		expected = 1
	}
	if generated != 1 || dispatched != expected {
		return Message{}, ErrGenerationUnknown
	}
	return response, nil
}

type einoTurnModel struct {
	generate func(context.Context) (*schema.Message, error)
}

func (m *einoTurnModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return m.generate(ctx)
}
func (m *einoTurnModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	output, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{output}), nil
}
func (m *einoTurnModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	clone := *m
	return &clone, nil
}

type einoProposalTool struct {
	definition AgentTool
	onDispatch func()
}

func (t *einoProposalTool) Info(context.Context) (*schema.ToolInfo, error) {
	parameters := map[string]*schema.ParameterInfo{"nodeIds": {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String, Enum: slices.Clone(t.definition.NodeIDs)}, Required: true}}
	if t.definition.Profile {
		for _, name := range []string{"profileId", "policySha256", "arguments", "timeoutSeconds"} {
			parameters[name] = &schema.ParameterInfo{Type: schema.String, Required: true}
		}
	}
	return &schema.ToolInfo{Name: t.definition.Name, Desc: t.definition.Description, ParamsOneOf: schema.NewParamsOneOfByParams(parameters)}, nil
}
func (t *einoProposalTool) InvokableRun(_ context.Context, arguments string, _ ...tool.Option) (string, error) {
	if !json.Valid([]byte(arguments)) {
		return "", ErrGenerationUnknown
	}
	t.onDispatch()
	return "Proposal submitted to the controller task run; await its actual result.", nil
}
