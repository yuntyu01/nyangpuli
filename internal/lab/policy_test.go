package lab

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/tarot"
)

func TestProductPolicyUsesOneTarotSource(t *testing.T) {
	l, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	in, err := l.Product(context.Background(), "tarot", "c1", []byte(`{"question":"q","question_type":"love","spread":"single-focus","cards":[{"card_id":3,"reversed":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(in.Sources, ",")
	if in.VariantID != "arcanite_product_v1" || strings.Contains(joined, "tarotoo") || !strings.Contains(joined, tarot.ProductPolicyID) {
		t.Fatalf("tarot product %s %v", in.VariantID, in.Sources)
	}
	if strings.Contains(in.UserPrompt, "tarotoo:") {
		t.Fatal("product prompt mixes Tarotoo meanings")
	}
	if _, err = l.Product(context.Background(), "unknown", "c1", []byte(`{}`)); err == nil {
		t.Fatal("unknown domain accepted")
	}
}

func TestProductPolicyExcludesExperimentalTiaohou(t *testing.T) {
	if os.Getenv("SAJU_INTEGRATION") != "1" {
		t.Skip("set SAJU_INTEGRATION=1 to run the pinned OhMySaju engine")
	}
	l, err := New("../..")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile("../../fixtures/saju.json")
	if err != nil {
		t.Fatal(err)
	}
	in, err := l.Product(context.Background(), "saju", "c1", input)
	if err != nil {
		t.Fatal(err)
	}
	if in.VariantID != SajuPolicyID || strings.Contains(strings.Join(in.Sources, ","), "yueyuan") || strings.Contains(in.UserPrompt, "yueyuan:tiao_hou") {
		t.Fatalf("saju product mixes experimental table: %v", in.Sources)
	}
}
