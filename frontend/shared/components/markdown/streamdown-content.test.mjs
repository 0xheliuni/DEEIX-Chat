import assert from "node:assert/strict";
import test from "node:test";

import { normalizeSafeHTMLBlockBlankLines } from "./streamdown-content.ts";

const CARD_HTML = `<div style="background:var(--card);padding:16px">
  <div><strong>First item</strong><br/><span>First body</span></div>

  <div><strong>Second item</strong><br/><span>Second body</span></div>

  <div><strong>Third item</strong><br/><span>Third body</span></div>
</div>`;

test("keeps balanced safe HTML in one CommonMark block", () => {
  const normalized = normalizeSafeHTMLBlockBlankLines(CARD_HTML);

  assert.notEqual(normalized, CARD_HTML);
  assert.equal(normalized.split("\n").length, CARD_HTML.split("\n").length);
  assert.equal((normalized.match(/<!-- deeix-html-gap -->/g) ?? []).length, 2);
  assert.equal(normalizeSafeHTMLBlockBlankLines(normalized), normalized);
});

test("normalizes a safe incomplete prefix only while streaming", () => {
  const incomplete = CARD_HTML.slice(0, CARD_HTML.lastIndexOf("</div>"));

  assert.equal(normalizeSafeHTMLBlockBlankLines(incomplete), incomplete);
  assert.match(
    normalizeSafeHTMLBlockBlankLines(incomplete, { allowIncomplete: true }),
    /<!-- deeix-html-gap -->/,
  );
});

test("does not rewrite Markdown literals or ordinary paragraph spacing", () => {
  const fenced = `\`\`\`html
<div>first

<div>second</div>
</div>
\`\`\``;
  const openFence = `\`\`\`html
<div>first

<div>second</div>`;
  const inline = "`<div>first\n\nsecond</div>`";
  const markdown = "first paragraph\n\nsecond paragraph";

  assert.equal(normalizeSafeHTMLBlockBlankLines(fenced), fenced);
  assert.equal(normalizeSafeHTMLBlockBlankLines(openFence, { allowIncomplete: true }), openFence);
  assert.equal(normalizeSafeHTMLBlockBlankLines(inline), inline);
  assert.equal(normalizeSafeHTMLBlockBlankLines(markdown), markdown);
});

test("leaves unsafe or malformed HTML untouched", () => {
  const unsafe = "<div>first\n\n<script>alert(1)</script></div>";
  const unsafePrefix = "<div>first\n\n<script";
  const unbalanced = "<div>first\n\n<div>second</div>";

  assert.equal(normalizeSafeHTMLBlockBlankLines(unsafe), unsafe);
  assert.equal(
    normalizeSafeHTMLBlockBlankLines(unsafePrefix, { allowIncomplete: true }),
    unsafePrefix,
  );
  assert.equal(normalizeSafeHTMLBlockBlankLines(unbalanced), unbalanced);
});

test("parses a greater-than sign inside a quoted attribute", () => {
  const source = '<div style="content: >">first\n\n<span>second</span></div>';

  assert.match(normalizeSafeHTMLBlockBlankLines(source), /<!-- deeix-html-gap -->/);
});
