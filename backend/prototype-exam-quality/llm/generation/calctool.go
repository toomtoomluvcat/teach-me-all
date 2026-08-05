package generation

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"protoexam/examgen"
)

// This is phase A of calculation-question generation: before the model is
// allowed to write anything, it works out the numbers with a calculator it
// cannot fake.
//
// Why this and not "generate, then check": both were built and measured on
// scb10x/typhoon2.5-qwen3-4b.
//
//   - Generate-then-check found the model getting its own arithmetic wrong in
//     4 of 5 calculation questions. Handing it back the discrepancy and asking
//     it to fix the question repaired 0 of 4, twice, on two documents. A 4B
//     model cannot reliably see its own error even when told exactly what it is.
//   - Tool calling asks something much easier: not "find your mistake" but
//     "don't make one". The model emits calc(expression), gets a number back,
//     and writes the question around a value it never had to compute.
//
// The arithmetic gate stays regardless. It is free, it is deterministic, and it
// re-verifies every stored question years later, which no tool call can.

const maxToolRounds = 4

var calcTool = Tool{
	Type: "function",
	Function: ToolFunction{
		Name:        "calc",
		Description: "Evaluate an arithmetic expression and return the number it produces. Use this for every calculation. Never do arithmetic yourself.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "plain arithmetic using numbers, + - * / ^, parentheses, pi, and only sin, cos, tan, sqrt, abs, exp, ln. No variables, units, or words. Trigonometric arguments are radians. Example: (20*sin(30*pi/180))/9.8",
				},
			},
			"required":             []string{"expression"},
			"additionalProperties": false,
		},
	},
}

const factsSystem = `You are preparing the arithmetic for exam questions about a passage.

You do not write questions here. You only work out numbers.

Call the calc tool once for each quantity an exam question about this passage
could reasonably ask for. Use only numbers that appear in the passage. You may
use sin, cos, tan, sqrt, abs, exp, ln, and pi when the source gives a matching
formula; trig arguments must be radians. Do not invent inputs and do not do
any arithmetic yourself — every number you report must come back from the tool.

When you have computed everything useful, reply with the single word DONE.
If the passage contains no numbers worth computing with, reply DONE immediately.`

// Fact is one arithmetic result the model asked for and Go computed.
type Fact struct {
	Expression string
	Value      float64
}

// ComputeFacts runs the tool loop over a chunk and returns the arithmetic that
// actually checked out. Expressions the evaluator rejects are dropped — the
// model is told so, and can try again within the remaining rounds.
func (g *Generator) ComputeFacts(ctx context.Context, c examgen.Chunk, forceCalc bool) ([]Fact, error) {
	if !forceCalc && !worthCalculating(c.Text) {
		return nil, nil
	}

	ctx = WithLabel(ctx, "calc-tool")
	var arith examgen.Arith
	msgs := []Message{
		{Role: "system", Content: factsSystem},
		{Role: "user", Content: fmt.Sprintf("Passage (page %d):\n\n%s", c.Page, c.Text)},
	}

	var facts []Fact
	seen := map[string]bool{}

	for round := 0; round < maxToolRounds; round++ {
		reply, err := g.c.ChatTools(ctx, g.model, msgs, []Tool{calcTool}, genOptions(8192, 0))
		if err != nil {
			return facts, err
		}
		if len(reply.ToolCalls) == 0 {
			return facts, nil
		}
		msgs = append(msgs, reply)

		for _, tc := range reply.ToolCalls {
			expr, _ := tc.Function.Arguments["expression"].(string)
			expr = strings.TrimSpace(expr)

			result := ""
			switch {
			case expr == "":
				result = "error: no expression given"
			case seen[expr]:
				result = "already computed; move on"
			default:
				v, err := arith.Eval(expr)
				if err != nil {
					result = "error: " + err.Error() + ". Use only numbers, + - * / ^, parentheses, pi, and the allowed math functions"
					break
				}
				seen[expr] = true
				facts = append(facts, Fact{Expression: expr, Value: v})
				result = trimNumber(v)
			}
			msgs = append(msgs, Message{Role: "tool", ToolName: "calc", ToolCallID: tc.ID, Content: result})
		}
	}
	return facts, nil
}

// FactsBlock renders verified arithmetic for the generation prompt.
func FactsBlock(facts []Fact) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nArithmetic already computed for you by a calculator. " +
		"These values are correct. Use them exactly as given and do not recalculate anything:\n")
	for _, f := range facts {
		fmt.Fprintf(&b, "  %s = %s\n", f.Expression, trimNumber(f.Value))
	}
	b.WriteString("If a question needs one of these, copy the expression into calculation.expression " +
		"and the value into calculation.expected, character for character.\n")
	return b.String()
}

// worthCalculating decides whether a chunk has arithmetic in it at all.
//
// The earlier test — three digits anywhere — passed essentially every page,
// because page numbers, section numbers and years are digits. The tool loop
// then spent 40 seconds across a prose document re-reading chunks to produce 16
// output tokens, all of them "DONE". Requiring several distinct numbers, at
// least one of which is not a small integer, separates a worked example from a
// page that merely has "หน้า 3" in the header.
func worthCalculating(s string) bool {
	var numbers []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			numbers = append(numbers, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsDigit(r) || (r == '.' && cur.Len() > 0) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	if len(numbers) < 3 {
		return false
	}
	for _, n := range numbers {
		if strings.Contains(n, ".") || len(strings.TrimLeft(n, "0")) >= 3 {
			return true
		}
	}
	return false
}

func trimNumber(v float64) string {
	s := fmt.Sprintf("%.6f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
