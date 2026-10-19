package repository

import (
	"testing"

	"packaging-ai/internal/domain"
)

func TestNormalizeBriefSnapshotMapsRichProductInfo(t *testing.T) {
	brief := domain.Brief{
		ProductInfo: map[string]any{
			"product_category":        "红茶",
			"product_name":            "祁门红茶",
			"target_customers":        "高端商务人士",
			"packaging_type":          "礼盒+内铁罐",
			"required_packaging_text": map[string]any{"食品名称": "祁门红茶", "净含量": "240克"},
			"regulatory_requirements": []any{"GB 7718"},
		},
		Material: "食品级铝箔袋",
		PackageSpec: map[string]any{
			"outer_box_size": "360mm×280mm×100mmH",
		},
		CompletionScore: 57,
	}
	if !normalizeBriefSnapshot(&brief) {
		t.Fatal("expected brief to be normalized")
	}
	if brief.Audience != "高端商务人士" || brief.ProductInfo["package_type"] != "礼盒+内铁罐" {
		t.Fatalf("stable fields not mapped: %#v", brief)
	}
	if len(brief.RequiredText) != 2 || len(brief.Constraints) != 1 || brief.CompletionScore != 100 {
		t.Fatalf("normalized brief = %#v", brief)
	}
}

func TestNormalizeBriefSnapshotMapsDocumentFieldAliases(t *testing.T) {
	brief := domain.Brief{ProductInfo: map[string]any{
		"category":                 "祁门红茶",
		"product_name":             "炒青本源",
		"target_customer":          "高端商务人士",
		"package_type":             "礼盒+内铁罐+小袋",
		"mandatory_packaging_text": map[string]any{"食品名称": "祁门红茶", "净含量": "240克"},
		"material_requirement":     "食品级铝箔袋",
	}, PackageSpec: map[string]any{"finished_size": "360mm×280mm×100mm"}}

	if !normalizeBriefSnapshot(&brief) {
		t.Fatal("expected document aliases to be normalized")
	}
	if brief.ProductInfo["product_category"] != "祁门红茶" || brief.Audience != "高端商务人士" || brief.Material != "食品级铝箔袋" {
		t.Fatalf("stable aliases not mapped: %#v", brief)
	}
	if len(brief.RequiredText) != 2 || brief.CompletionScore != 100 {
		t.Fatalf("normalized brief = %#v", brief)
	}
}
