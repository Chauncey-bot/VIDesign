package workflow

import (
	"testing"

	"packaging-ai/internal/domain"
)

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
