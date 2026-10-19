package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"packaging-ai/internal/domain"
)

func TestTaskProposalCountAcceptsJSONNumbers(t *testing.T) {
	for _, value := range []any{9, float64(9), json.Number("9")} {
		if got := taskProposalCount(value); got != 9 {
			t.Fatalf("proposal count for %T = %d", value, got)
		}
	}
}

func TestProductNameForProposalUsesSpecifiedName(t *testing.T) {
	used := map[string]struct{}{}
	proposal := domain.DesignProposal{Title: "山岚玉青"}
	if got := productNameForProposal(proposal, "松萝茶·山岚玉青", used, 1); got != "松萝茶·山岚玉青" {
		t.Fatalf("product name = %q", got)
	}
}

func TestProductNameForProposalCreatesUniqueNamesWhenOmitted(t *testing.T) {
	used := map[string]struct{}{}
	first := productNameForProposal(domain.DesignProposal{Title: "山岚玉青"}, "", used, 1)
	second := productNameForProposal(domain.DesignProposal{Title: "山岚玉青"}, "", used, 2)
	if first == second || first != "山岚玉青系列" || second != "山岚玉青系列 2" {
		t.Fatalf("generated names = %q, %q", first, second)
	}
}

func TestProtectedAssetIDsPreserveUploadedLogoReferences(t *testing.T) {
	ids := protectedAssetIDs([]any{"logo-1", "logo-1", "logo-2"})
	if len(ids) != 2 || ids[0] != "logo-1" || ids[1] != "logo-2" {
		t.Fatalf("protected logo ids = %#v", ids)
	}
}

func TestProtectedLogoSurfacePromptRequiresPhysicalIntegration(t *testing.T) {
	for _, phrase := range []string{
		"specified physical finish",
		"safe margin from edges",
		"matching perspective, curvature",
		"contact shadow",
		"original logo file",
	} {
		if !strings.Contains(protectedBrandMarkSurfacePrompt, phrase) {
			t.Fatalf("protected brand mark prompt missing %q: %s", phrase, protectedBrandMarkSurfacePrompt)
		}
	}
	for _, phrase := range []string{"watermark", "pasted PNG", "white halo", "mark crossing a fold or seam"} {
		if !strings.Contains(protectedBrandMarkSurfaceNegative, phrase) {
			t.Fatalf("protected brand mark negative prompt missing %q: %s", phrase, protectedBrandMarkSurfaceNegative)
		}
	}
}
