"""OpenAI tool schemas for agents.

defines Event Designer tools only.
"""

EVENT_DESIGNER_TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "register_detection_rule",
            "description": (
                "Register a custom visual detection rule with VideoDB. "
                "Each rule fires an alert when its natural-language prompt matches "
                "what appears on screen."
            ),
            "strict": True,
            "parameters": {
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "label": {
                        "type": "string",
                        "description": (
                            "Short snake_case identifier, e.g. "
                            "visual_teaching_aid_visible, code_or_terminal_visible, "
                            "concept_transition, talking_head_only."
                        ),
                    },
                    "natural_language_prompt": {
                        "type": "string",
                        "description": (
                            "Detection prompt for VideoDB. Include what to detect and "
                            "what to exclude."
                        ),
                    },
                    "product_action": {
                        "type": "string",
                        "description": (
                            "What the product should do when this rule fires, e.g. "
                            "capture screenshot, start new section, suppress screenshot."
                        ),
                    },
                },
                "required": ["label", "natural_language_prompt", "product_action"],
            },
        },
    }
]

