package tarot

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSessionPersistsDrawAndRejectsDuplicatePosition(t *testing.T) {
	e := engineForTest(t)
	s, err := e.NewSession("관계를 어떻게 돌아볼까요?", "love", "single-focus")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, d := range s.Deck {
		if seen[d.CardID] {
			t.Fatalf("duplicate card %d", d.CardID)
		}
		seen[d.CardID] = true
	}
	if len(seen) != 78 {
		t.Fatalf("deck has %d cards", len(seen))
	}
	if err := e.Select(&s, 7); err != nil {
		t.Fatal(err)
	}
	original, err := e.SessionRequest(s)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored Session
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	result, err := e.SessionRequest(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, result) {
		t.Fatal("restored session changed card or orientation")
	}
	if err := e.Select(&restored, 7); err == nil {
		t.Fatal("completed session accepted selection")
	}
}

func TestSessionRejectsInvalidOrForgedState(t *testing.T) {
	e := engineForTest(t)
	if _, err := e.NewSession("", "love", "single-focus"); err == nil {
		t.Fatal("empty question accepted")
	}
	s, err := e.NewSession("질문", "love", "single-focus")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Select(&s, 78); err == nil {
		t.Fatal("invalid position accepted")
	}
	if _, err := e.SessionRequest(s); err == nil {
		t.Fatal("incomplete session accepted")
	}
	s.Selected = []int{99}
	if _, err := e.SessionRequest(s); err == nil {
		t.Fatal("forged position accepted")
	}
}

func TestThreeCardSelectionDoesNotRepeat(t *testing.T) {
	e := engineForTest(t)
	s, err := e.NewSession("무엇을 돌아볼까요?", "love", "past-present-future")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Select(&s, 3); err != nil {
		t.Fatal(err)
	}
	if err := e.Select(&s, 3); err == nil {
		t.Fatal("duplicate position accepted")
	}
	if err := e.Select(&s, 4); err != nil {
		t.Fatal(err)
	}
	if err := e.Select(&s, 5); err != nil {
		t.Fatal(err)
	}
	request, err := e.SessionRequest(s)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int]bool{}
	for _, card := range request.Cards {
		if ids[card.CardID] {
			t.Fatal("duplicate card")
		}
		ids[card.CardID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("got %d cards", len(ids))
	}
}
