package gates

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Everything here is deterministic and costs no model call.
//
// It exists because 14 questions that passed all four original gates were read
// by hand and 5 were outright broken. Every defect below is one of those, and
// none of them needed a model to spot:
//
//   - a stem truncated mid-word, "ทฤษฎีความคิดสร้างสรร", with no question in it
//   - a correct choice that was the stem repeated verbatim
//   - a corrupted token inside a Thai word, "สร้างสรningerช่วย"
//   - four choices that each restated the whole scenario, so the answer could be
//     found by scanning for the numbers instead of computing
//   - one choice two and a half times the length of another
//
// The blind judge passed all of them as "interpretable". A 4B model asked
// whether something reads sensibly says yes; string length does not.

// bannedPhrases point at material the reader cannot see. The generation prompt
// forbids them and the model uses them anyway.
var bannedPhrases = []string{
	"ตามข้อความในหนังสือ", "ในข้อความต้นฉบับ", "ตามข้อความข้างต้น", "จากข้อความข้างต้น",
	"ตามเนื้อหาข้างต้น", "ในเนื้อหาข้างต้น", "ตามที่กล่าวไว้ข้างต้น", "ในบทความนี้",
	"according to the passage", "in the passage above", "in the text above",
	"the passage states", "as mentioned in the text", "based on the passage",
	"from the text above", "according to the text",
	// Found by reading generated biology questions: the list only caught
	// phrases that begin with a preposition, and the model reached for a
	// participle instead — "described in the passage" sailed through.
	"in the passage", "in this passage", "the passage describes",
	"described in the text", "in the given text", "mentioned above",
}

// pedagogyPhrases identify questions about how the source is taught or graded,
// rather than questions about the subject matter. Teacher books legitimately
// contain these phrases, so quote grounding alone cannot distinguish them from
// useful exam content.
var pedagogyPhrases = []string{
	"จุดประสงค์การเรียนรู้", "แนวการวัดและประเมินผล", "การประเมินด้านความรู้",
	"ประเมินจากอะไร", "กิจกรรมและแบบทดสอบ", "การศึกษาวีดิทัศน์",
	"learning objective", "assessment guideline", "assessment criteria",
	"teaching activity", "video and discussion",
}

// questionWords let a stem be recognised as a question. Thai written questions
// frequently carry no question mark, so punctuation alone is not enough.
var questionWords = []string{
	"อะไร", "ใด", "ไหน", "ทำไม", "อย่างไร", "เท่าใด", "เท่าไร", "กี่", "เพราะเหตุใด", "ข้อใด",
	"what", "which", "how", "why", "when", "who", "where", "calculate", "find the",
	"simplify", "solve", "evaluate", "convert", "write the answer", "determine", "reduce",
}

// Tunables. Each was set from the failure it was written for, not from taste.
const (
	minStemRunes  = 15
	maxChoiceSkew = 2.5 // longest choice ÷ shortest
	stemEchoRatio = 0.6 // share of the stem that may appear inside one choice
	// sharedChoiceRunes stays high on purpose. Choices sharing a lead-in —
	// every Thai option starting "ความสามารถในการ", every English one starting
	// "the ability to" — is parallel construction, which is good writing, not a
	// defect. Lowering this to catch scenario-restating choices flagged two
	// well-written questions instead. checkChoicesEchoStem is the right tool for
	// that case; this one only catches near-identical options.
	sharedChoiceRunes = 40
	// choiceEchoesStemRunes: if every choice repeats this much of the stem, the
	// options are restatements of the question and the student answers by
	// matching numbers rather than by knowing anything.
	choiceEchoesStemRunes = 20
	// duplicateRatio applies to the literal-text fallback. Embedding similarity
	// is used instead whenever an embedder is configured, because two questions
	// can ask the same thing in entirely different words and share almost no
	// substring — which is exactly what happened.
	duplicateRatio = 0.85
	// duplicateCosine sits between the two clusters measured with bge-m3 on
	// Thai: reworded duplicates land at 0.95+, different questions from the
	// same chapter at 0.60-0.64. It is deliberately near the top of that gap —
	// wrongly dropping a good question costs more than keeping a near-duplicate.
	duplicateCosine = 0.90
)

// gateWellFormed runs every structural check and reports the first failure,
// because one broken question does not need five reasons.
func gateWellFormed(q Question) GateResult {
	res := GateResult{Gate: GateWellFormed, Deterministic: true}

	// Most specific defect first, so the reported reason is the useful one.
	for _, check := range []func(Question) string{
		checkStemIsAQuestion,
		checkNoBannedPhrase,
		checkNoPedagogyMetadata,
		checkChoicesPresent,
		checkNoMojibake,
		checkChoicesDistinct,
		checkChoiceDoesNotEchoStem,
		checkAllChoicesEchoStem,
		checkChoiceLengthSkew,
		checkChoicesNotFormulaic,
	} {
		if reason := check(q); reason != "" {
			res.Reason = reason
			return res
		}
	}

	res.Pass = true
	res.Reason = "structurally sound"
	return res
}

// checkStemIsAQuestion catches the truncated stem. A stem cut off mid-word
// keeps whatever came before the cut, so length alone will not find it — but
// the cut removes the interrogative, and a question without one is not a
// question.
func checkStemIsAQuestion(q Question) string {
	stem := strings.TrimSpace(q.Stem)
	if RuneLen(stem) < minStemRunes {
		return fmt.Sprintf("stem is only %d runes: %q", RuneLen(stem), stem)
	}
	if strings.HasSuffix(stem, "?") || strings.HasSuffix(stem, "？") {
		return ""
	}
	lower := strings.ToLower(stem)
	for _, w := range questionWords {
		if strings.Contains(lower, w) {
			return ""
		}
	}
	return fmt.Sprintf("stem asks nothing — no question mark and no question word: %q", excerpt(stem, 80))
}

func checkNoBannedPhrase(q Question) string {
	lower := strings.ToLower(q.Stem)
	for _, p := range bannedPhrases {
		if strings.Contains(lower, strings.ToLower(p)) {
			return fmt.Sprintf("stem points at material the reader cannot see: %q", p)
		}
	}
	return ""
}

func checkNoPedagogyMetadata(q Question) string {
	lower := strings.ToLower(q.Stem)
	for _, p := range pedagogyPhrases {
		if strings.Contains(lower, strings.ToLower(p)) {
			return fmt.Sprintf("stem asks about teacher-guide metadata instead of subject matter: %q", p)
		}
	}
	return ""
}

func checkChoicesPresent(q Question) string {
	if len(q.Choices) < 2 {
		return fmt.Sprintf("only %d choices", len(q.Choices))
	}
	for i, c := range q.Choices {
		if strings.TrimSpace(c.Content) == "" {
			return fmt.Sprintf("choice %d is empty", i+1)
		}
	}
	if q.CorrectIndex() < 0 {
		return "not exactly one choice is marked correct"
	}
	return ""
}

func checkChoicesDistinct(q Question) string {
	seen := map[string]int{}
	for i, c := range q.Choices {
		k := squeeze(strings.ToLower(c.Content))
		if j, dup := seen[k]; dup {
			return fmt.Sprintf("choices %d and %d are the same text", j+1, i+1)
		}
		seen[k] = i
	}
	return ""
}

// checkChoiceDoesNotEchoStem catches an answer that is the question again.
func checkChoiceDoesNotEchoStem(q Question) string {
	stem := squeeze(strings.ToLower(q.Stem))
	if RuneLen(stem) == 0 {
		return ""
	}
	for i, c := range q.Choices {
		choice := squeeze(strings.ToLower(c.Content))
		shared := longestCommonSubstring(stem, choice)
		if float64(RuneLen(shared)) >= stemEchoRatio*float64(RuneLen(stem)) {
			return fmt.Sprintf("choice %d repeats the stem rather than answering it", i+1)
		}
	}
	return ""
}

// checkAllChoicesEchoStem catches options that are each a restatement of the
// scenario with one detail swapped:
//
//	stem:  What is the interest on 1200 baht at 7% for one year?
//	  1.   The interest earned in one year on a deposit of 1200 baht at 7% is 84 baht
//	  2.   The interest earned in two years on a deposit of 1200 baht at 7% is 168 baht
//	  3.   The interest earned in one year on a deposit of 1500 baht at 7% is 105 baht
//
// A student answers that by finding the option whose numbers match the stem.
// They never compute anything. Note this is about every choice echoing the
// stem — choices merely resembling each other is parallel construction and is
// deliberately not penalised.
func checkAllChoicesEchoStem(q Question) string {
	stem := squeeze(strings.ToLower(q.Stem))
	if len(q.Choices) < 3 || RuneLen(stem) < 30 {
		return ""
	}
	worst := 1 << 30
	for _, c := range q.Choices {
		shared := RuneLen(longestCommonSubstring(stem, squeeze(strings.ToLower(c.Content))))
		if shared < choiceEchoesStemRunes {
			return ""
		}
		worst = min(worst, shared)
	}
	return fmt.Sprintf("every choice restates at least %d runes of the question — the answer can be found by matching, not by knowing", worst)
}

// checkChoiceLengthSkew catches options that are not parallel. A choice much
// longer than its siblings stands out on the page, and length is the oldest
// tell in multiple choice writing.
func checkChoiceLengthSkew(q Question) string {
	if len(q.Choices) < 2 {
		return ""
	}
	lens := make([]int, len(q.Choices))
	for i, c := range q.Choices {
		lens[i] = RuneLen(strings.TrimSpace(c.Content))
	}
	sorted := append([]int(nil), lens...)
	sort.Ints(sorted)
	shortest, longest := sorted[0], sorted[len(sorted)-1]
	if shortest == 0 {
		return "a choice is empty"
	}
	// Very short choices — bare numbers — are legitimately uneven. "84" against
	// "110.25" is a 3x ratio and perfectly fine.
	if longest < 25 {
		return ""
	}
	if float64(longest)/float64(shortest) > maxChoiceSkew {
		return fmt.Sprintf("choices are not parallel: longest is %d runes, shortest is %d", longest, shortest)
	}
	return ""
}

// checkChoicesNotFormulaic catches four options that are the same sentence with
// a number swapped. The student answers by matching the numbers in the stem
// instead of by knowing anything.
func checkChoicesNotFormulaic(q Question) string {
	if len(q.Choices) < 3 {
		return ""
	}
	// Only meaningful for choices long enough to be sentences. Short numeric
	// options — "84", "102.5 baht" — are supposed to look alike.
	for _, c := range q.Choices {
		if RuneLen(strings.TrimSpace(c.Content)) < 40 {
			return ""
		}
	}
	shared := squeeze(strings.ToLower(q.Choices[0].Content))
	for _, c := range q.Choices[1:] {
		shared = longestCommonSubstring(shared, squeeze(strings.ToLower(c.Content)))
		if RuneLen(shared) < sharedChoiceRunes {
			return ""
		}
	}
	return fmt.Sprintf("every choice restates the same %d runes — they differ only in the detail being asked about", RuneLen(shared))
}

// checkNoMojibake catches a corrupted token. Latin letters wedged between Thai
// characters with no space on either side is not a loanword, it is damage:
// Thai writes loanwords with a space or in parentheses.
func checkNoMojibake(q Question) string {
	fields := append([]string{q.Stem}, choiceTexts(q)...)
	for i, s := range fields {
		where := "stem"
		if i > 0 {
			where = fmt.Sprintf("choice %d", i)
		}
		if strings.ContainsRune(s, '�') {
			return where + " contains an unreadable character"
		}
		if run := thaiLatinSandwich(s); run != "" {
			return fmt.Sprintf("%s has a corrupted token: %q wedged inside Thai text", where, run)
		}
	}
	return ""
}

func choiceTexts(q Question) []string {
	out := make([]string, len(q.Choices))
	for i, c := range q.Choices {
		out[i] = c.Content
	}
	return out
}

// thaiLatinSandwich returns a Latin run with Thai immediately on both sides.
func thaiLatinSandwich(s string) string {
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if !isThaiRune(r[i]) {
			continue
		}
		j := i + 1
		for j < len(r) && isLatinLetter(r[j]) {
			j++
		}
		if j > i+2 && j < len(r) && isThaiRune(r[j]) {
			return string(r[i+1 : j])
		}
	}
	return ""
}

func isThaiRune(r rune) bool    { return r >= 0x0E00 && r <= 0x0E7F }
func isLatinLetter(r rune) bool { return unicode.IsLetter(r) && r < 0x0250 }

// longestCommonSubstring is O(n*m) and runs on strings a few hundred runes
// long, a handful of times per question. Fine here; do not lift it into a hot
// path without thinking.
func longestCommonSubstring(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return ""
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	best, end := 0, 0

	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best, end = cur[j], i
				}
			} else {
				cur[j] = 0
			}
		}
		prev, cur = cur, prev
	}
	return string(ra[end-best : end])
}

// SimilarTo reports whether two questions are near-duplicates. Used by the
// pipeline against questions already accepted, because a lesson that asks
// "what is creativity" three times has three questions and one question's worth
// of coverage.
func similarQuestions(q, other Question) bool {
	a := squeeze(strings.ToLower(q.Stem))
	b := squeeze(strings.ToLower(other.Stem))
	if a == "" || b == "" {
		return false
	}
	shared := RuneLen(longestCommonSubstring(a, b))
	shorter := min(RuneLen(a), RuneLen(b))
	return float64(shared) >= duplicateRatio*float64(shorter)
}

// gateDistinct rejects a question that asks what an already-accepted one asks.
//
// This one cannot live in RunCheapGates: it is the only check that depends on the
// rest of the exam rather than on the question alone. Reading a real run turned
// up "what is creativity" three times in twelve questions, and two more that
// were the same question about linguistic intelligence.
func gateDistinct(q Question, accepted []Question, vec []float32, acceptedVecs [][]float32) GateResult {
	res := GateResult{Gate: GateDistinct, Deterministic: true}

	// Embeddings first when they are available. Literal text comparison cannot
	// see that "ความฉลาดทางด้านภาษามีลักษณะอย่างไร" and "ลักษณะสำคัญของความฉลาด
	// ทางด้านภาษามีอะไรบ้าง" are one question asked twice.
	if len(vec) > 0 {
		for i, ov := range acceptedVecs {
			if i >= len(accepted) || len(ov) == 0 {
				continue
			}
			if sim := cosine(vec, ov); sim >= duplicateCosine {
				res.Reason = fmt.Sprintf("%.0f%% the same question as accepted #%d: %q",
					sim*100, i+1, excerpt(accepted[i].Stem, 55))
				return res
			}
		}
	}

	for i, other := range accepted {
		if similarQuestions(q, other) {
			res.Reason = fmt.Sprintf("asks the same thing as accepted question %d: %q", i+1, excerpt(other.Stem, 60))
			return res
		}
	}

	res.Pass = true
	res.Reason = "distinct from the questions already accepted"
	return res
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func excerpt(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
