// Markdown rendering for model output (the shared layer — consumed by
// the chat thread; the sidebar may follow). marked → HTML, then
// DOMPurify sanitizes: model output is UNTRUSTED (prompt-injected
// pages, tool results echoed back), so nothing executable survives —
// scripts, event handlers, javascript: URLs, and remote images are
// stripped (local-first: no fetches triggered by content).
import { marked } from "marked";
import DOMPurify from "dompurify";

marked.setOptions({ gfm: true, breaks: true });

// The allowlist: prose semantics only. No images from remote origins
// (tracking pixels / exfil), no iframes, no styles.
const ALLOWED_TAGS = [
  "p", "br", "hr", "h1", "h2", "h3", "h4", "h5", "h6",
  "ul", "ol", "li", "blockquote", "pre", "code", "em", "strong", "del", "s",
  "a", "table", "thead", "tbody", "tr", "th", "td", "sup", "sub", "kbd", "mark",
];
const ALLOWED_ATTR = ["href", "title", "start", "align"];

export function renderMarkdown(src: string): string {
  if (!src) return "";
  const html = marked.parse(src, { async: false });
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOWED_URI_REGEXP: /^(?:https?|mailto):/i,
    FORBID_TAGS: ["style", "iframe", "img", "form", "input", "button", "video", "audio"],
    FORBID_ATTR: ["style", "onerror", "onload", "onclick"],
  });
}
