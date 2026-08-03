package examgen

import "testing"

// The titles here are verbatim from the cached 217-concept graph of the Thai
// IPST biology teacher book, so a change to the phrase lists is measured against
// real pass-1 output rather than invented strings.
func TestIsPedagogyConceptSeparatesApparatusFromSubjectMatter(t *testing.T) {
	apparatus := []string{
		"การวัดและประเมินผล",
		"แนวการวัดและประเมินผลด้านความรู้และทักษะ",
		"เฉลยแบบฝึกหัดท้ายบทที่ 13",
		"เฉลยคำถามท้ายกิจกรรม",
		"แบบทดสอบแบบเลือกตอบ",
		"แบบประเมินคุณลักษณะด้านจิตวิทยาศาสตร์",
		"เกณฑ์การประเมินสมรรถภาพด้านการเขียน",
		"แนวทางการให้คะแนนการเขียนรายงานการทดลอง",
		"แนวการจัดการเรียนรู้",
		"การวิเคราะห์ผลการเรียนรู้",
		"การเตรียมตัวล่วงหน้าสำหรับครู",
		"ผังมโนทัศน์ บทที่ 17",
		"เวลาที่ใช้",
		"ชวนคิด",
		"การประเมินการนำเสนอผลงาน",
		"Assessment criteria",
		"Answer key",
	}
	for _, title := range apparatus {
		if !IsPedagogyConcept(title) {
			t.Errorf("IsPedagogyConcept(%q) = false, want true", title)
		}
	}

	// Dropping a concept also detaches its chunks from every lesson, so these
	// must survive. Lab activities and teacher-knowledge sidebars carry the real
	// subject matter that the surrounding apparatus is written about.
	subject := []string{
		"กิจกรรม 15.1 โครงสร้างของหัวใจสัตว์เลี้ยงลูกด้วยน้ำนม",
		"ความรู้เพิ่มเติมสำหรับครู: Osmolarity และ countercurrent multiplier system",
		"ผลการทดลองกิจกรรมจำลองการทำงานของกล้ามเนื้อกะบังลม",
		"การย่อยอาหารในกระเพาะอาหาร",
		"เวลาที่ใช้ในการย่อยอาหาร",
		"การขับถ่ายอุจจาระ",
		"Gas exchange in the alveoli",
	}
	for _, title := range subject {
		if IsPedagogyConcept(title) {
			t.Errorf("IsPedagogyConcept(%q) = true, want false", title)
		}
	}
}

func TestBuildEvidenceGraphDropsPedagogyConceptsAndTheirOnlyChunks(t *testing.T) {
	chunks := []Chunk{
		{ID: "p1-c0", Page: 1, Text: "digestion"},
		{ID: "p2-c1", Page: 2, Text: "assessment guidance"},
	}
	perChunk := [][]string{
		{"การย่อยอาหารในปาก", "แนวการวัดและประเมินผล"},
		{"เฉลยแบบฝึกหัดท้ายบทที่ 13"},
	}

	if got := CountPedagogyTopics(perChunk); got != 2 {
		t.Fatalf("CountPedagogyTopics() = %d, want 2", got)
	}

	graph := BuildEvidenceGraph(chunks, perChunk)
	if len(graph.Concepts) != 1 || graph.Concepts[0].Title != "การย่อยอาหารในปาก" {
		t.Fatalf("concepts = %#v, want only the subject-matter concept", graph.Concepts)
	}
	if len(graph.Concepts[0].ChunkIDs) != 1 || graph.Concepts[0].ChunkIDs[0] != "p1-c0" {
		t.Fatalf("provenance = %#v, want the chunk that taught the concept", graph.Concepts[0])
	}
	// The apparatus concept was the only label on p2-c1, so no edge may claim it
	// and the chunk stays unattached to any lesson.
	if len(graph.Edges) != 0 {
		t.Fatalf("edges = %#v, want none: a dropped concept cannot be an endpoint", graph.Edges)
	}
}
