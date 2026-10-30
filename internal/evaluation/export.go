// Package evaluation exports blinded reading comparisons, not accuracy scores.
package evaluation

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuntyu01/nyangpuli-lab/internal/generation"
)

type BlindAnswer struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}
type BlindCase struct {
	ID       string          `json:"case_id"`
	Question string          `json:"question"`
	Context  json.RawMessage `json:"context,omitempty"`
	Answers  []BlindAnswer   `json:"answers"`
}
type BlindSet struct {
	SchemaVersion int         `json:"schema_version"`
	Cases         []BlindCase `json:"cases"`
	Instructions  []string    `json:"instructions"`
}
type KeyEntry struct {
	CaseID       string   `json:"case_id"`
	Label        string   `json:"label"`
	VariantID    string   `json:"variant_id"`
	PromptSHA256 string   `json:"prompt_sha256"`
	Sources      []string `json:"sources,omitempty"`
}
type AnswerKey struct {
	Seed    int64      `json:"seed"`
	Entries []KeyEntry `json:"entries"`
}

// Build hides condition metadata, not semantic clues in generated prose. A
// neutral reviewer should check that prose does not name its source condition.
// Failed, truncated, or prompt-only runs cannot enter a reading-quality trial.
func Build(results []generation.Result, seed int64) (BlindSet, AnswerKey, error) {
	set := BlindSet{SchemaVersion: 1, Instructions: []string{
		"모델·조건·출처를 알려주지 않고 답변을 비교하세요. 문장 안의 단서까지 자동으로 지워지지는 않습니다.",
		"미제공 사실 생성, 상대 마음 단정, 명시적 거절 무시, 현재·과거 혼동을 각각 세고 문제 문장을 적으세요.",
		"도움·공감 평가는 오류 및 실제 사건 적중과 별도입니다. 동점과 판단 불가를 허용하세요.",
		"answer-key.json과 raw-results.json은 평가자에게 제공하지 마세요. 평가를 잠근 뒤에만 조건을 공개하세요.",
	}}
	key := AnswerKey{Seed: seed}
	if len(results) == 0 {
		return BlindSet{}, AnswerKey{}, errors.New("no results to blind")
	}
	groups := map[string][]generation.Result{}
	seen := map[string]bool{}
	var firstConfig []byte
	var returnedModel string
	for _, r := range results {
		if r.Status != "generated" || strings.TrimSpace(r.Text) == "" || r.FinishReason != "stop" || r.Config == nil {
			return BlindSet{}, AnswerKey{}, errors.New("only completed generated answers with configuration can be blinded")
		}
		if r.Input.CaseID == "" || r.Input.VariantID == "" || r.Input.Question == "" {
			return BlindSet{}, AnswerKey{}, errors.New("missing case, variant, or question")
		}
		id := r.Input.CaseID + "\x00" + r.Input.VariantID
		if seen[id] {
			return BlindSet{}, AnswerKey{}, errors.New("duplicate case/variant result")
		}
		seen[id] = true
		cfg, _ := json.Marshal(r.Config)
		if firstConfig == nil {
			firstConfig = cfg
			returnedModel = r.ReturnedModel
		} else if !bytes.Equal(firstConfig, cfg) {
			return BlindSet{}, AnswerKey{}, errors.New("comparison requires identical provider/model/generation configuration")
		}
		if r.ReturnedModel != returnedModel {
			return BlindSet{}, AnswerKey{}, errors.New("provider returned different model versions across conditions")
		}
		groups[r.Input.CaseID] = append(groups[r.Input.CaseID], r)
	}
	cases := make([]string, 0, len(groups))
	for id := range groups {
		cases = append(cases, id)
	}
	sort.Strings(cases)
	rng := rand.New(rand.NewSource(seed))
	for _, id := range cases {
		rows := groups[id]
		if len(rows) < 2 || len(rows) > 26 {
			return BlindSet{}, AnswerKey{}, fmt.Errorf("case %s requires 2 to 26 variants", id)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Input.VariantID < rows[j].Input.VariantID })
		question := rows[0].Input.Question
		context, err := canonicalContext(rows[0].Input.CaseContext)
		if err != nil {
			return BlindSet{}, AnswerKey{}, fmt.Errorf("case %s has invalid context", id)
		}
		for _, r := range rows {
			if r.Input.Question != question {
				return BlindSet{}, AnswerKey{}, fmt.Errorf("case %s has mismatched questions", id)
			}
			candidate, err := canonicalContext(r.Input.CaseContext)
			if err != nil {
				return BlindSet{}, AnswerKey{}, fmt.Errorf("case %s has invalid context", id)
			}
			if !bytes.Equal(context, candidate) {
				return BlindSet{}, AnswerKey{}, fmt.Errorf("case %s has mismatched shared context", id)
			}
		}
		rng.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
		bc := BlindCase{ID: id, Question: question, Context: context}
		for i, r := range rows {
			label := string(rune('A' + i))
			bc.Answers = append(bc.Answers, BlindAnswer{label, r.Text})
			key.Entries = append(key.Entries, KeyEntry{id, label, r.Input.VariantID, r.PromptSHA256, r.Input.Sources})
		}
		set.Cases = append(set.Cases, bc)
	}
	return set, key, nil
}

// Canonicalize object key order and whitespace without rounding large numeric
// identifiers through float64. Exact JSON number notation remains significant.
func canonicalContext(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("invalid shared context JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// Export creates a new private directory and refuses to overwrite an existing
// run. The files are not encrypted; keep real personal data out of this lab.
// Review customizes the reviewer sheet. Instructions are appended to the
// default blinding instructions; Columns replaces the default CSV header.
type Review struct {
	Columns      []string
	Instructions []string
}

var defaultColumns = []string{"case_id", "label", "invented_fact_count", "partner_mind_certainty_count", "boundary_violation_count", "stale_fact_count", "helpfulness_1_5", "empathy_1_5", "situation_fit_1_5", "specific_action_1_5", "exception_compliance_1_5", "repetition_observed", "problem_quote", "reviewer_id"}

func Export(dir string, results []generation.Result, seed int64) error {
	return ExportReview(dir, results, seed, Review{})
}

func ExportReview(dir string, results []generation.Result, seed int64, review Review) error {
	set, key, err := Build(results, seed)
	if err != nil {
		return err
	}
	set.Instructions = append(set.Instructions, review.Instructions...)
	columns := defaultColumns
	if len(review.Columns) > 0 {
		columns = review.Columns
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("output directory is required")
	}
	files := map[string][]byte{}
	for name, v := range map[string]any{"blind.json": set, "answer-key.json": key, "raw-results.json": results} {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		files[name] = append(b, '\n')
	}
	var buffer bytes.Buffer
	w := csv.NewWriter(&buffer)
	_ = w.Write(columns)
	for _, c := range set.Cases {
		for _, a := range c.Answers {
			row := make([]string, len(columns))
			row[0], row[1] = c.ID, a.Label
			_ = w.Write(row)
		}
	}
	w.Flush()
	if w.Error() != nil {
		return w.Error()
	}
	files["review.csv"] = buffer.Bytes()
	parent := filepath.Dir(filepath.Clean(dir))
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("create new output directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	for name, b := range files {
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return err
		}
	}
	complete = true
	return nil
}
