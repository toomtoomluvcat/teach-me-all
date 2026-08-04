import unittest

from pdfx.docling_helper import (
    PAGE_BREAK,
    align_page_sections,
    combine_page_markdown,
    markdown_to_plain,
    native_text_to_plain,
    resolve_formula,
    resolve_ocr,
)


class CombinePageMarkdownTests(unittest.TestCase):
    def test_aligns_serializer_sections_around_empty_page(self) -> None:
        aligned = align_page_sections(
            page_numbers=[1, 2, 3],
            sections=["page one", "page three"],
            pages_with_content={1, 3},
        )

        self.assertEqual(["page one", "", "page three"], aligned)

    def test_preserves_empty_physical_pages(self) -> None:
        combined = combine_page_markdown(
            [
                "# Page one\n\n![one](../assets/one.png)\n",
                "",
                "# Page three\n",
            ]
        )

        sections = combined.split(PAGE_BREAK)
        self.assertEqual(3, len(sections))
        self.assertIn("# Page one", sections[0])
        self.assertEqual("", sections[1].strip())
        self.assertIn("# Page three", sections[2])
        self.assertIn("(assets/one.png)", combined)
        self.assertNotIn("(../assets/one.png)", combined)


class ExtractionModeTests(unittest.TestCase):
    def test_auto_disables_ocr_for_native_pdf(self) -> None:
        self.assertEqual((False, "off"), resolve_ocr("auto", 2, 2))

    def test_auto_enables_ocr_for_scanned_pdf(self) -> None:
        self.assertEqual((True, "on"), resolve_ocr("auto", 0, 2))

    def test_formula_auto_is_a_hint_not_a_hidden_heavy_pass(self) -> None:
        enabled, mode, hint = resolve_formula("auto", "The equation is written mathematically.")
        self.assertFalse(enabled)
        self.assertEqual("off", mode)
        self.assertTrue(hint)

    def test_markdown_plain_preserves_latex_and_removes_placeholder(self) -> None:
        plain = markdown_to_plain("A relation is $$F=ma$$.\n\n<!-- formula-not-decoded -->")
        self.assertIn("$$F=ma$$", plain)
        self.assertNotIn("formula-not-decoded", plain)

    def test_native_text_plain_collapses_pdf_line_wraps(self) -> None:
        self.assertEqual("mass and weight", native_text_to_plain("mass\r\n and weight"))


if __name__ == "__main__":
    unittest.main()
