export type RenderSegment =
  | {
      type: "markdown";
      content: string;
    }
  | {
      type: "thinking";
      content: string;
      incomplete: boolean;
    };

type ParseStreamdownSegmentsOptions = {
  normalizeHTMLVisualFences?: boolean;
  parseThinking?: boolean;
};

export function normalizeContent(input: unknown): string {
  if (typeof input === "string") {
    return input;
  }

  if (typeof input === "number" || typeof input === "boolean" || typeof input === "bigint") {
    return String(input);
  }

  if (input == null) {
    return "";
  }

  if (Array.isArray(input)) {
    return input.map((item) => normalizeContent(item)).filter(Boolean).join("\n");
  }

  if (typeof input === "object") {
    const maybeRecord = input as Record<string, unknown>;
    const textValue = maybeRecord.content ?? maybeRecord.text ?? maybeRecord.message;
    if (typeof textValue === "string") {
      return textValue;
    }

    try {
      return JSON.stringify(input, null, 2);
    } catch {
      return "";
    }
  }

  return "";
}

const MARKDOWN_LITERAL_FRAGMENT_RE = /(```[\s\S]*?```|~~~[\s\S]*?~~~|`[^`\n]*`)/g;
const HTML_VISUAL_MARKDOWN_FENCE_RE = /(^|\n)([ \t]{0,3})(```|~~~)[ \t]*(?:(?:markdown|md)[^\n]*)?\n([\s\S]*?)\n[ \t]*\3[ \t]*(?=\n|$)/gi;
const HTML_VISUAL_FRAGMENT_RE = /^\s*<(?:div|section|article|aside|main|details|table)\b[\s\S]*<\/(?:div|section|article|aside|main|details|table)>\s*$/i;
const HTML_VISUAL_STYLE_RE = /\sstyle\s*=\s*["'][^"']{8,}["']/i;
const HTML_TAG_RE = /<\/?[A-Za-z][^>\n]*>/g;
const SAFE_HTML_BLOCK_ROOT_RE = /(^|\n)([ \t]{0,3})<(article|aside|details|div|main|section|table)\b/gi;
const SAFE_HTML_BLOCK_TAGS = new Set([
  "a",
  "article",
  "aside",
  "b",
  "blockquote",
  "br",
  "caption",
  "code",
  "col",
  "colgroup",
  "dd",
  "del",
  "details",
  "div",
  "dl",
  "dt",
  "em",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "hr",
  "i",
  "img",
  "li",
  "main",
  "mark",
  "ol",
  "p",
  "section",
  "small",
  "span",
  "strong",
  "sub",
  "summary",
  "sup",
  "table",
  "tbody",
  "td",
  "tfoot",
  "th",
  "thead",
  "tr",
  "u",
  "ul",
]);
const SAFE_HTML_VOID_TAGS = new Set(["br", "col", "hr", "img"]);
const HTML_BLANK_LINE_RE = /(\r?\n)[ \t]*(?=\r?\n)/g;
const INLINE_DOLLAR_MATH_RE = /(^|[^\\$])\$([^$\n]{1,800})\$/g;
const ESCAPED_INLINE_DOLLAR_MATH_RE = /\\\$([^$\n]{1,400})\\\$/g;
const DISPLAY_DOLLAR_MATH_RE = /(\${2,})([\s\S]*?)(\1)/g;
const CURRENCY_DOLLAR_RE = /(^|[^\\$])\$((?:\d{1,3}(?:,\d{3})+|\d+\.\d{1,2}))(?!\$)(?=\b)/g;
const MARKDOWN_DISPLAY_MATH_RE = /(?:^|\n)\s*\$\$[\s\S]+?\$\$|\\\[[\s\S]+?\\\]|\\begin\{[a-z*]+\}/i;
const MARKDOWN_INLINE_MATH_RE = /(^|[^\\$])\$[^$\n]{1,400}\$/;

function isMarkdownLiteralFragment(fragment: string): boolean {
  return fragment.startsWith("```") || fragment.startsWith("~~~") || fragment.startsWith("`");
}

function mapMarkdownTextFragments(source: string, transform: (fragment: string) => string): string {
  return source
    .split(MARKDOWN_LITERAL_FRAGMENT_RE)
    .map((fragment) => {
      if (!fragment || isMarkdownLiteralFragment(fragment)) {
        return fragment;
      }
      return transform(fragment);
    })
    .join("");
}

function looksLikeLatexMathContent(value: string): boolean {
  const trimmedValue = value.trim();
  if (!trimmedValue || /^\d+(?:[.,]\d+)?$/.test(trimmedValue)) {
    return false;
  }

  return (
    /\\[A-Za-z]+/.test(trimmedValue) ||
    /[\^_{}=<>+\-*/]/.test(trimmedValue) ||
    (trimmedValue.includes("|") && /[A-Za-z\\Α-ω]|[\^_{}=<>+\-*/]/.test(trimmedValue)) ||
    /^[A-Za-z]$/.test(trimmedValue) ||
    /[Α-ω]/.test(trimmedValue)
  );
}

function normalizeLatexPipes(mathContent: string): string {
  return mathContent.replace(/(^|[^\\])\|/g, "$1\\vert{}");
}

function isEscapedCharacter(source: string, index: number): boolean {
  let slashCount = 0;
  for (let cursor = index - 1; cursor >= 0 && source[cursor] === "\\"; cursor -= 1) {
    slashCount += 1;
  }
  return slashCount % 2 === 1;
}

function getDollarMathDelimiterLength(source: string, index: number): number {
  if (source[index] !== "$" || isEscapedCharacter(source, index) || source[index - 1] === "$") {
    return 0;
  }

  if (source[index + 1] === "$" && source[index + 2] !== "$") {
    return 2;
  }

  return source[index + 1] === "$" ? 0 : 1;
}

function normalizeDollarMathContent(mathContent: string, inline: boolean): string {
  const normalizedContent = inline ? mathContent.replace(/\s*\n\s*/g, " ") : mathContent;
  return normalizeLatexPipes(normalizedContent);
}

const PARAGRAPH_BREAK_RE = /\n[ \t]*\n/;
const HTML_BLOCK_TAG_RE = /<\/?\s*(?:div|p|section|article|aside|main|blockquote|ul|ol|li|table|thead|tbody|tr|th|td|h[1-6]|pre|hr|details|summary|nav|header|footer|figure|figcaption)\b/i;

function normalizeDollarMathSegments(source: string): string {
  if (!source.includes("$")) {
    return source;
  }

  let normalizedSource = "";
  let consumedUntil = 0;

  for (let index = 0; index < source.length; index += 1) {
    const delimiterLength = getDollarMathDelimiterLength(source, index);
    if (!delimiterLength) {
      continue;
    }

    const openingDelimiterIndex = index;
    let closingDelimiterIndex = -1;
    for (let cursor = openingDelimiterIndex + delimiterLength; cursor < source.length; cursor += 1) {
      if (getDollarMathDelimiterLength(source, cursor) === delimiterLength) {
        closingDelimiterIndex = cursor;
        break;
      }
    }

    if (closingDelimiterIndex < 0) {
      break;
    }

    const mathContent = source.slice(openingDelimiterIndex + delimiterLength, closingDelimiterIndex);
    const inline = delimiterLength === 1;

    if (inline && (PARAGRAPH_BREAK_RE.test(mathContent) || HTML_BLOCK_TAG_RE.test(mathContent))) {
      index = closingDelimiterIndex + delimiterLength - 1;
      continue;
    }

    const shouldNormalize =
      (mathContent.includes("|") || (inline && mathContent.includes("\n"))) &&
      looksLikeLatexMathContent(mathContent);

    if (shouldNormalize) {
      normalizedSource += source.slice(consumedUntil, openingDelimiterIndex + delimiterLength);
      normalizedSource += normalizeDollarMathContent(mathContent, inline);
      normalizedSource += source.slice(closingDelimiterIndex, closingDelimiterIndex + delimiterLength);
      consumedUntil = closingDelimiterIndex + delimiterLength;
    }

    index = closingDelimiterIndex + delimiterLength - 1;
  }

  if (!consumedUntil) {
    return source;
  }

  return normalizedSource + source.slice(consumedUntil);
}

function normalizeLatexDelimitersInText(source: string): string {
  return source
    .replace(/\\\[\s*\n?([\s\S]*?)\n?\s*\\\]/g, (_, mathContent: string) => `$$\n${mathContent.trim()}\n$$`)
    .replace(/\\\(([\s\S]*?)\\\)/g, (_, mathContent: string) => `$${mathContent.trim()}$`)
    .replace(ESCAPED_INLINE_DOLLAR_MATH_RE, (match: string, mathContent: string) => {
      const trimmedMathContent = mathContent.trim();
      return looksLikeLatexMathContent(trimmedMathContent) ? `$${trimmedMathContent}$` : match;
    });
}

export function normalizeMathDelimiters(source: string): string {
  if (!source) {
    return source;
  }

  const shouldNormalizeDelimiters = source.includes("\\(") || source.includes("\\[") || source.includes("\\$");
  const hasDollarMath = source.includes("$");
  if (!shouldNormalizeDelimiters && !hasDollarMath) {
    return source;
  }

  return mapMarkdownTextFragments(source, (fragment) => {
    const normalizedFragment = shouldNormalizeDelimiters ? normalizeLatexDelimitersInText(fragment) : fragment;
    return normalizedFragment.includes("$") ? normalizeDollarMathSegments(normalizedFragment) : normalizedFragment;
  });
}

export function normalizeCurrencyDollars(source: string): string {
  if (!source.includes("$")) {
    return source;
  }

  return mapMarkdownTextFragments(source, (fragment) =>
    fragment.replace(CURRENCY_DOLLAR_RE, (_match: string, prefix: string, amount: string) => `${prefix}&#36;${amount}`),
  );
}

export function containsMarkdownMath(source: string): boolean {
  if (!source.includes("$") && !source.includes("\\") && !source.includes("\\begin")) {
    return false;
  }

  return MARKDOWN_DISPLAY_MATH_RE.test(source) || MARKDOWN_INLINE_MATH_RE.test(source);
}

const LATEX_UNICODE_SYMBOLS: Array<[RegExp, string]> = [
  [/→/g, " \\to "],
  [/←/g, " \\leftarrow "],
  [/⇒/g, " \\Rightarrow "],
  [/⇐/g, " \\Leftarrow "],
  [/↔/g, " \\leftrightarrow "],
  [/⇔/g, " \\Leftrightarrow "],
];

const THINKING_LIKE_HTML_TAG_RE = /<\/?\s*think[\w-]*\b[^>]*>/gi;

function escapeHtmlTag(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function escapeThinkingLikeHtmlTags(source: string): string {
  if (!source || !/<\/?\s*think/i.test(source)) {
    return source;
  }

  return mapMarkdownTextFragments(source, (fragment) => fragment.replace(THINKING_LIKE_HTML_TAG_RE, escapeHtmlTag));
}

function normalizeLatexSymbols(mathContent: string): string {
  return LATEX_UNICODE_SYMBOLS.reduce(
    (normalizedContent, [pattern, replacement]) => normalizedContent.replace(pattern, replacement),
    mathContent,
  );
}

export function normalizeLatexUnicodeSymbols(source: string): string {
  if (!source || !/[→←⇒⇐↔⇔]/.test(source)) {
    return source;
  }

  return mapMarkdownTextFragments(source, (fragment) =>
    fragment
      .replace(DISPLAY_DOLLAR_MATH_RE, (match, openingDelimiter: string, mathContent: string, closingDelimiter: string) => {
        if (!mathContent) {
          return match;
        }

        return `${openingDelimiter}${normalizeLatexSymbols(mathContent)}${closingDelimiter}`;
      })
      .replace(INLINE_DOLLAR_MATH_RE, (match: string, prefix: string, mathContent: string) => {
        if (!mathContent) {
          return match;
        }

        return `${prefix}$${normalizeLatexSymbols(mathContent)}$`;
      }),
  );
}

export function normalizeMermaidBlocks(source: string): string {
  if (!source.includes("```mermaid")) {
    return source;
  }

  return source.replace(/```mermaid([\s\S]*?)```/gi, (block) =>
    block.replace(/<br\s*>/gi, "<br/>").replace(/<br\s*\/\s*>/gi, "<br/>"),
  );
}

export function normalizeEscapedHTMLAttributeQuotes(source: string): string {
  if (!source.includes('\\"') && !source.includes("\\'")) {
    return source;
  }

  return mapMarkdownTextFragments(source, (fragment) =>
    fragment.replace(HTML_TAG_RE, (tag) => tag.replace(/\\"/g, '"').replace(/\\'/g, "'")),
  );
}

export function normalizeHTMLVisualMarkdownFences(source: string): string {
  if (!source.includes("```") && !source.includes("~~~")) {
    return source;
  }

  return source.replace(
    HTML_VISUAL_MARKDOWN_FENCE_RE,
    (match: string, prefix: string, _indent: string, _fence: string, code: string) => {
      const trimmedCode = code.trim();
      if (!HTML_VISUAL_FRAGMENT_RE.test(trimmedCode) || !HTML_VISUAL_STYLE_RE.test(trimmedCode)) {
        return match;
      }
      return `${prefix}${trimmedCode}`;
    },
  );
}

type ParsedHTMLTag = {
  closing: boolean;
  end: number;
  name: string | null;
  selfClosing: boolean;
};

function parseHTMLTagAt(source: string, start: number): ParsedHTMLTag | null {
  if (source.startsWith("<!--", start)) {
    const commentEnd = source.indexOf("-->", start + 4);
    return commentEnd < 0
      ? null
      : { closing: false, end: commentEnd + 3, name: null, selfClosing: true };
  }

  if (source[start] !== "<") {
    return null;
  }

  let cursor = start + 1;
  const closing = source[cursor] === "/";
  if (closing) {
    cursor += 1;
  }

  const nameMatch = /^[A-Za-z][A-Za-z0-9-]*/.exec(source.slice(cursor));
  if (!nameMatch) {
    return null;
  }
  const name = nameMatch[0].toLowerCase();
  cursor += nameMatch[0].length;

  let quote: "\"" | "'" | null = null;
  for (; cursor < source.length; cursor += 1) {
    const character = source[cursor];
    if (quote) {
      if (character === quote) {
        quote = null;
      }
      continue;
    }
    if (character === "\"" || character === "'") {
      quote = character;
      continue;
    }
    if (character === ">") {
      const rawTag = source.slice(start, cursor + 1);
      return {
        closing,
        end: cursor + 1,
        name,
        selfClosing: !closing && /\/\s*>$/.test(rawTag),
      };
    }
  }

  return null;
}

function findBalancedSafeHTMLBlockEnd(
  source: string,
  start: number,
  allowIncomplete: boolean,
): number | null {
  const stack: string[] = [];
  let cursor = start;
  let firstTag = true;

  while (cursor < source.length) {
    const tagStart = source.indexOf("<", cursor);
    if (tagStart < 0) {
      return allowIncomplete && stack.length > 0 ? source.length : null;
    }

    const tag = parseHTMLTagAt(source, tagStart);
    if (!tag) {
      const incompleteTagName = /^<\/?([A-Za-z][A-Za-z0-9-]*)/.exec(source.slice(tagStart))?.[1]?.toLowerCase();
      if (
        incompleteTagName &&
        !Array.from(SAFE_HTML_BLOCK_TAGS).some((safeTag) => safeTag.startsWith(incompleteTagName))
      ) {
        return null;
      }
      if (allowIncomplete && incompleteTagName && !source.slice(tagStart).includes(">")) {
        return stack.length > 0 ? source.length : null;
      }
      cursor = tagStart + 1;
      continue;
    }
    cursor = tag.end;

    if (tag.name == null) {
      continue;
    }
    if (!SAFE_HTML_BLOCK_TAGS.has(tag.name)) {
      return null;
    }
    if (firstTag && tag.closing) {
      return null;
    }
    firstTag = false;

    if (tag.closing) {
      if (stack.at(-1) !== tag.name) {
        return null;
      }
      stack.pop();
      if (stack.length === 0) {
        return tag.end;
      }
      continue;
    }

    if (!tag.selfClosing && !SAFE_HTML_VOID_TAGS.has(tag.name)) {
      stack.push(tag.name);
    }
  }

  return allowIncomplete && stack.length > 0 ? source.length : null;
}

function isInsideOpenMarkdownFence(source: string, index: number): boolean {
  const fencePattern = /(^|\n)[ \t]{0,3}(`{3,}|~{3,})([^\n]*)/g;
  let opening: { character: string; length: number } | null = null;

  for (const match of source.slice(0, index).matchAll(fencePattern)) {
    const marker = match[2];
    if (!opening) {
      opening = { character: marker[0], length: marker.length };
      continue;
    }
    if (
      marker[0] === opening.character &&
      marker.length >= opening.length &&
      match[3].trim() === ""
    ) {
      opening = null;
    }
  }

  return opening != null;
}

/**
 * Keeps balanced allowlisted HTML cards, plus their safe in-flight prefix,
 * inside one CommonMark raw-HTML block. A blank line normally terminates type-6
 * HTML blocks, which makes later nested elements render as literal source.
 * Fixed comments are invisible after parsing and preserve the source line count.
 */
export function normalizeSafeHTMLBlockBlankLines(
  source: string,
  { allowIncomplete = false }: { allowIncomplete?: boolean } = {},
): string {
  if (!source || !HTML_BLANK_LINE_RE.test(source)) {
    HTML_BLANK_LINE_RE.lastIndex = 0;
    return source;
  }
  HTML_BLANK_LINE_RE.lastIndex = 0;

  return mapMarkdownTextFragments(source, (fragment) => {
    const rootPattern = new RegExp(SAFE_HTML_BLOCK_ROOT_RE.source, SAFE_HTML_BLOCK_ROOT_RE.flags);
    const output: string[] = [];
    let cursor = 0;

    for (const match of fragment.matchAll(rootPattern)) {
      if (match.index == null) {
        continue;
      }
      const blockStart = match.index + match[1].length + match[2].length;
      if (blockStart < cursor || isInsideOpenMarkdownFence(fragment, blockStart)) {
        continue;
      }

      const blockEnd = findBalancedSafeHTMLBlockEnd(fragment, blockStart, allowIncomplete);
      if (blockEnd == null) {
        continue;
      }

      output.push(fragment.slice(cursor, blockStart));
      output.push(
        fragment
          .slice(blockStart, blockEnd)
          .replace(HTML_BLANK_LINE_RE, "$1<!-- deeix-html-gap -->"),
      );
      cursor = blockEnd;
      rootPattern.lastIndex = blockEnd;
    }

    if (cursor === 0) {
      return fragment;
    }
    output.push(fragment.slice(cursor));
    return output.join("");
  });
}

export function parseStreamdownSegments(
  source: string,
  { normalizeHTMLVisualFences = true, parseThinking = true }: ParseStreamdownSegmentsOptions = {},
): RenderSegment[] {
  if (!source) {
    return [];
  }

  const normalizedSource = normalizeHTMLVisualFences ? normalizeHTMLVisualMarkdownFences(source) : source;
  const segments: RenderSegment[] = [];

  const thinkingBlock = parseThinking ? parseLeadingThinkingBlock(normalizedSource) : null;
  if (!thinkingBlock) {
    if (normalizedSource.trim()) {
      segments.push({
        type: "markdown",
        content: escapeThinkingLikeHtmlTags(normalizedSource),
      });
    }
    return segments;
  }

  segments.push({
    type: "thinking",
    content: thinkingBlock.content,
    incomplete: thinkingBlock.incomplete,
  });

  const tail = normalizedSource.slice(thinkingBlock.end);
  if (tail.trim()) {
    segments.push({
      type: "markdown",
      content: escapeThinkingLikeHtmlTags(tail),
    });
  }

  return segments;
}

function parseLeadingThinkingBlock(
  source: string,
): { content: string; end: number; incomplete: boolean } | null {
  const firstContentIndex = source.search(/\S/);
  if (firstContentIndex < 0) {
    return null;
  }

  const openingSource = source.slice(firstContentIndex);
  const openingMatch = /^<(think|thinking)\b[^>]*>/i.exec(openingSource);
  if (!openingMatch) {
    return null;
  }
  if (openingMatch[0].slice(0, -1).trimEnd().endsWith("/")) {
    return null;
  }

  const tagName = openingMatch[1].toLowerCase();
  const contentStart = firstContentIndex + openingMatch[0].length;
  const closingMatch = new RegExp(`</${tagName}\\s*>`, "i").exec(source.slice(contentStart));
  if (!closingMatch) {
    return {
      content: source.slice(contentStart),
      end: source.length,
      incomplete: true,
    };
  }

  const closeStart = contentStart + closingMatch.index;
  const closeEnd = closeStart + closingMatch[0].length;
  return {
    content: source.slice(contentStart, closeStart),
    end: closeEnd,
    incomplete: false,
  };
}
