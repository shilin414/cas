"""Real layout regression; run with Python + existing Playwright/Edge (no installs).

    python frontend/src/components/Chat/__tests__/ArtifactMarkdown.layout.test.py

Vitest separately checks that fenced Markdown renders as pre > code. This test
checks that markup against the production CSS in a real layout engine, since
jsdom cannot measure scrollWidth or clientWidth.
"""
import os
from pathlib import Path
import unittest

from playwright.sync_api import sync_playwright


class ArtifactMarkdownLayoutTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.playwright = sync_playwright().start()
        cls.browser = cls.playwright.chromium.launch(
            channel=os.environ.get("PLAYWRIGHT_CHANNEL", "msedge"), headless=True
        )
        cls.css = (Path(__file__).resolve().parents[1] / "ArtifactMarkdown.css").read_text(encoding="utf-8")

    @classmethod
    def tearDownClass(cls):
        cls.browser.close()
        cls.playwright.stop()

    def test_long_fences_scroll_without_widening_the_page(self):
        page = self.browser.new_page()
        self.addCleanup(page.close)
        line = "const value = '" + "x" * 2000 + "';"
        for width in (320, 768, 1280):
            for layout in ("block", "flex", "grid"):
                for nesting in ("plain", "list", "quote"):
                    with self.subTest(width=width, layout=layout, nesting=nesting):
                        page.set_viewport_size({"width": width, "height": 600})
                        code = f"<pre><code>{line}\n  indented line\n</code></pre>"
                        if nesting == "list":
                            code = f"<ul><li>{code}</li></ul>"
                        elif nesting == "quote":
                            code = f"<blockquote>{code}</blockquote>"
                        page.set_content(
                            f"<style>{self.css}</style>"
                            f'<main style="display:{layout}">'
                            f'<div class="artifact-markdown">{code}</div></main>'
                        )
                        measurements = page.evaluate("""() => {
                            const pre = document.querySelector('pre');
                            const root = document.documentElement;
                            pre.scrollLeft = 100;
                            return {
                                pageWidth: root.clientWidth,
                                pageScrollWidth: root.scrollWidth,
                                codeWidth: pre.clientWidth,
                                codeScrollWidth: pre.scrollWidth,
                                scrollLeft: pre.scrollLeft,
                                whiteSpace: getComputedStyle(pre).whiteSpace,
                                text: pre.textContent,
                            };
                        }""")
                        self.assertLessEqual(measurements["pageScrollWidth"], measurements["pageWidth"] + 1, measurements)
                        self.assertGreater(measurements["codeScrollWidth"], measurements["codeWidth"])
                        self.assertGreater(measurements["scrollLeft"], 0)
                        self.assertEqual(measurements["whiteSpace"], "pre")
                        self.assertEqual(measurements["text"], line + "\n  indented line\n")


if __name__ == "__main__":
    unittest.main()
