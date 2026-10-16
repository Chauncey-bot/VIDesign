package agent

import (
	"context"
	"fmt"
	"strings"
)

// Intake is a deterministic first implementation. It keeps the workflow
// usable without an external LLM and can later be replaced by an LLM-backed
// implementation that returns the same RunOutput schema.
type Intake struct{}

func NewIntake() *Intake { return &Intake{} }

func (a *Intake) Name() string { return "intake" }

func (a *Intake) Run(_ context.Context, input RunInput) (RunOutput, error) {
	message, _ := input.Payload["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return RunOutput{}, fmt.Errorf("message is required")
	}

	brief, _ := input.Payload["brief"].(map[string]any)
	if brief == nil {
		brief = map[string]any{}
	}
	patch := map[string]any{}
	lower := strings.ToLower(message)
	if strings.Contains(message, "茶") || strings.Contains(lower, "tea") {
		patch["product_category"] = "茶饮"
	}
	if strings.Contains(message, "红茶") {
		patch["product_name"] = "红茶"
	}
	if strings.Contains(message, "年轻") || strings.Contains(lower, "young") {
		patch["audience"] = "年轻消费者"
	}
	if strings.Contains(message, "高端") || strings.Contains(lower, "premium") {
		patch["positioning"] = "高端"
	}
	if strings.Contains(message, "礼盒") {
		patch["package_type"] = "礼盒"
	}
	if len(patch) == 0 {
		patch["customer_description"] = message
	}

	for key, value := range patch {
		brief[key] = value
	}
	missing := missingQuestions(brief)
	score := completionScore(brief)
	assistant := "我已记录这条需求。"
	if len(missing) > 0 {
		assistant += "为了形成完整的包装 Brief，还需要补充：" + strings.Join(missing, "、") + "。"
	} else {
		assistant += "当前 Brief 已达到生成方案所需的完整度。"
	}

	return RunOutput{
		AgentName: a.Name(),
		Payload: map[string]any{
			"brief_patch":       patch,
			"brief":             brief,
			"assistant_message": assistant,
			"next_questions":    missing,
			"completion_score":  score,
		},
	}, nil
}

func missingQuestions(brief map[string]any) []string {
	questions := []struct{ key, label string }{
		{"product_category", "产品品类"},
		{"audience", "目标客户"},
		{"package_type", "包装类型"},
		{"required_text", "包装必填文字"},
	}
	var missing []string
	for _, question := range questions {
		value, ok := brief[question.key]
		if !ok || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			missing = append(missing, question.label)
		}
	}
	return missing
}

func completionScore(brief map[string]any) int {
	keys := []string{"product_category", "product_name", "audience", "package_type", "required_text", "material", "package_spec"}
	filled := 0
	for _, key := range keys {
		if value, ok := brief[key]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			filled++
		}
	}
	return filled * 100 / len(keys)
}
