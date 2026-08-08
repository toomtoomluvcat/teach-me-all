package app

import (
	"fmt"
	"slices"
	"strings"
)

// The web UI lets a student pick a cognitive skill and a difficulty. Neither
// is a free-standing pipeline parameter: the contract builder derives both
// from the run directive (evidence.coverageDirectiveTargets), so the picker
// has to resolve to a directive the pipeline already understands.
//
// The directives are therefore not composed here. Every combination below maps
// to one of the subject-neutral benchmark cases, which are the exact strings
// the measured runs in docs/HANDOFF.md used. A combination with no measured
// directive is not offered rather than invented — an untested instruction is
// how a picker quietly stops matching what the gates check.

type examStyle struct {
	Skill        string   `json:"skill"`
	Label        string   `json:"label"`
	Hint         string   `json:"hint"`
	Difficulties []string `json:"difficulties"`
}

// examStyles is the picker, in the order it should be shown.
func examStyles() []examStyle {
	return []examStyle{
		{
			Skill:        "random",
			Label:        "สุ่มตามเนื้อหา",
			Hint:         "ปล่อยให้ระบบหมุนสลับ recall / understanding / application ตามที่บทเรียนรองรับ",
			Difficulties: []string{""},
		},
		{
			Skill:        "recall",
			Label:        "จำและระบุข้อเท็จจริง",
			Hint:         "ถามข้อเท็จจริงที่เนื้อหาบอกตรง ๆ ระดับยากจะทำให้ตัวลวงเป็นข้อความจริงจากที่อื่นในบท",
			Difficulties: []string{"", "hard"},
		},
		{
			Skill:        "understanding",
			Label:        "เข้าใจและตีความ",
			Hint:         "ตีความ อธิบาย จำแนก หรือเทียบความสัมพันธ์ที่เนื้อหาระบุไว้",
			Difficulties: []string{""},
		},
		{
			Skill:        "application",
			Label:        "ประยุกต์ใช้กับสถานการณ์ใหม่",
			Hint:         "เอาความสัมพันธ์ในบทไปใช้กับสถานการณ์ที่เปลี่ยนเงื่อนไข",
			Difficulties: []string{"easy", "medium", "hard"},
		},
		{
			Skill:        "analysis",
			Label:        "วิเคราะห์เชื่อมโยงสองข้อเท็จจริง",
			Hint:         "ต้องใช้ข้อเท็จจริงจากสองจุดที่แยกกันในบท ถ้ารู้แค่จุดเดียวจะตอบไม่ได้",
			Difficulties: []string{"easy", "medium", "hard"},
		},
		{
			Skill:        "calculation",
			Label:        "คำนวณด้วยตัวเลขในบท",
			Hint:         "ทุกข้อต้องมีเลขจริงจากเนื้อหาและนิพจน์ที่ Go ตรวจซ้ำได้",
			Difficulties: []string{""},
		},
		{
			Skill:        "error-finding",
			Label:        "หาที่ผิดในวิธีทำ",
			Hint:         "โจทย์โชว์วิธีทำที่ผิดหนึ่งจุด ให้เลือกว่าผิดตรงไหน",
			Difficulties: []string{""},
		},
	}
}

// benchmarkSelectionFor names the measured case for one skill/difficulty pair.
// An empty selection means no directive at all, which is the pipeline's own
// rotation across the forms each atom supports.
func benchmarkSelectionFor(skill, difficulty string) (string, error) {
	skill = strings.ToLower(strings.TrimSpace(skill))
	difficulty = strings.ToLower(strings.TrimSpace(difficulty))
	if skill == "" || skill == "random" {
		return "", nil
	}
	for _, style := range examStyles() {
		if style.Skill != skill {
			continue
		}
		if !containsStyleDifficulty(style.Difficulties, difficulty) {
			return "", fmt.Errorf("ยังไม่มีชุดคำสั่งที่วัดผลแล้วสำหรับ %s ระดับ %q", skill, difficulty)
		}
		switch skill {
		case "recall":
			if difficulty == "hard" {
				return "recall-hard", nil
			}
			return "recall", nil
		case "understanding", "calculation", "error-finding":
			return skill, nil
		case "application", "analysis":
			return skill + "-" + difficulty, nil
		}
	}
	return "", fmt.Errorf("ไม่รู้จักทักษะ %q", skill)
}

func containsStyleDifficulty(allowed []string, difficulty string) bool {
	return slices.Contains(allowed, difficulty)
}

// examDirective resolves the picker to the generation directive and the
// force-calc flag that go with it.
func examDirective(skill, difficulty, lessonTitle string) (directive string, forceCalc bool, err error) {
	selection, err := benchmarkSelectionFor(skill, difficulty)
	if err != nil {
		return "", false, err
	}
	if selection == "" {
		return "", false, nil
	}
	cases, err := genericBenchmarkCases(selection, lessonTitle)
	if err != nil {
		return "", false, err
	}
	if len(cases) != 1 {
		return "", false, fmt.Errorf("expected one benchmark case for %q, got %d", selection, len(cases))
	}
	return cases[0].Directive, cases[0].ForceCalc, nil
}
