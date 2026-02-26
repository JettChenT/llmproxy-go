import { createHighlighter } from "shiki";

const SUPPORTED_LANGUAGES = new Set([
  "json",
  "javascript",
  "typescript",
  "markdown",
  "xml",
  "yaml",
  "bash",
  "plaintext",
]);

let highlighterPromise: ReturnType<typeof createHighlighter> | null = null;

function getHighlighter() {
  if (!highlighterPromise) {
    highlighterPromise = createHighlighter({
      themes: ["github-dark"],
      langs: [...SUPPORTED_LANGUAGES],
    });
  }
  return highlighterPromise;
}

export function detectCodeLanguage(raw: string) {
  const trimmed = raw.trim();
  if (!trimmed) {
    return "plaintext";
  }

  if (
    (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]"))
  ) {
    return "json";
  }

  if (trimmed.startsWith("<") && trimmed.includes(">")) {
    return "xml";
  }

  return "plaintext";
}

function normalizeLanguage(language: string) {
  if (SUPPORTED_LANGUAGES.has(language)) {
    return language;
  }
  return "plaintext";
}

export async function highlightToHTML(raw: string, language: string) {
  const highlighter = await getHighlighter();
  const safeLanguage = normalizeLanguage(language);
  return highlighter.codeToHtml(raw || "(empty)", {
    lang: safeLanguage,
    theme: "github-dark",
  });
}
