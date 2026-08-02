import unittest

from pdfx.docling_helper import PAGE_BREAK, align_page_sections, combine_page_markdown


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


if __name__ == "__main__":
    unittest.main()
