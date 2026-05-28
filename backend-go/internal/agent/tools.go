package agent

import (
	"encoding/json"

	openai "github.com/sashabaranov/go-openai"
)

// EventDesignerTools returns the OpenAI tool definitions for the Event Designer
// agent. It is a direct port of Python EVENT_DESIGNER_TOOLS.
func EventDesignerTools() []openai.Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"label": map[string]any{
				"type": "string",
				"description": "Short snake_case identifier, e.g. " +
					"visual_teaching_aid_visible, code_or_terminal_visible, " +
					"concept_transition, talking_head_only.",
			},
			"natural_language_prompt": map[string]any{
				"type": "string",
				"description": "Detection prompt for VideoDB. Include what to detect and " +
					"what to exclude.",
			},
			"product_action": map[string]any{
				"type": "string",
				"description": "What the product should do when this rule fires, e.g. " +
					"capture screenshot, start new section, suppress screenshot.",
				"enum": []string{
					"capture screenshot",
					"start new section",
					"append evidence",
					"suppress screenshot",
				},
			},
		},
		"required": []string{"label", "natural_language_prompt", "product_action"},
	}

	schemaBytes, _ := json.Marshal(schema)

	return []openai.Tool{
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name: "register_detection_rule",
				Description: "Register a custom visual detection rule with VideoDB. " +
					"Each rule fires an alert when its natural-language prompt matches " +
					"what appears on screen.",
				Parameters: json.RawMessage(schemaBytes),
				Strict:     true,
			},
		},
	}
}
