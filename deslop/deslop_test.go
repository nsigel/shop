package deslop

import (
	"testing"

	"github.com/saucesteals/shop"
)

func TestEvaluateProductKnownBrand(t *testing.T) {
	t.Parallel()

	product := &shop.Product{
		Title: "Anker USB-C Cable",
		Brand: "Anker",
	}

	eval := EvaluateProduct(product, DefaultOptions())
	if eval.Product.Verdict != VerdictKnown {
		t.Fatalf("expected known verdict, got %s", eval.Product.Verdict)
	}
	if eval.Filtered {
		t.Fatalf("expected known brand to be kept")
	}
}

func TestEvaluateProductFlaggedBrand(t *testing.T) {
	t.Parallel()

	product := &shop.Product{
		Title: "HORUSDY Drill Bit Set",
		Brand: "HORUSDY",
	}

	eval := EvaluateProduct(product, DefaultOptions())
	if eval.Product.Verdict != VerdictFlagged {
		t.Fatalf("expected flagged verdict, got %s", eval.Product.Verdict)
	}
	if !eval.Filtered {
		t.Fatalf("expected flagged brand to be filtered")
	}
}

func TestEvaluateProductUnbranded(t *testing.T) {
	t.Parallel()

	product := &shop.Product{
		Title: "Magnetic Screwdriver Set",
	}

	eval := EvaluateProduct(product, DefaultOptions())
	if eval.Product.Verdict != VerdictUnbranded {
		t.Fatalf("expected unbranded verdict, got %s", eval.Product.Verdict)
	}
	if !eval.Filtered {
		t.Fatalf("expected unbranded product to be filtered at standard level")
	}
}

func TestEvaluateProductSellerVerdict(t *testing.T) {
	t.Parallel()

	product := &shop.Product{
		Title:  "Socket Organizer",
		Brand:  "Tekton",
		Seller: &shop.Seller{Name: "HORUSDY Direct"},
	}

	eval := EvaluateProduct(product, DefaultOptions())
	if eval.Seller == nil {
		t.Fatalf("expected seller verdict")
	}
	if eval.Seller.Verdict != VerdictFlagged {
		t.Fatalf("expected flagged seller verdict, got %s", eval.Seller.Verdict)
	}
}
