package workflow

import (
	"testing"

	"packaging-ai/internal/domain"
)

func TestNormalizeProductDetailsMapsFlatDocumentAliases(t *testing.T) {
	brief := domain.Brief{ProductInfo: map[string]any{
		"category":                 "祁门红茶",
		"product_name":             "炒青本源",
		"target_customer":          "高端商务人士",
		"package_type":             "礼盒+内铁罐+小袋",
		"mandatory_packaging_text": map[string]any{"食品名称": "祁门红茶"},
		"material_requirement":     "食品级铝箔袋",
		"package_spec":             map[string]any{"finished_size": "360mm×280mm×100mm"},
	}}

	normalizeProductDetails(&brief)
	if brief.ProductInfo["product_category"] != "祁门红茶" || brief.Audience != "高端商务人士" || brief.Material != "食品级铝箔袋" {
		t.Fatalf("stable aliases not mapped: %#v", brief)
	}
	if len(brief.RequiredText) != 1 || briefCompletionScore(brief) != 100 {
		t.Fatalf("normalized brief = %#v", brief)
	}
}
