export interface PromptPart {
  text: string;
  emphasized: boolean;
}

// The caller supplies the server-approved visible target. Never infer a word
// from sentence structure or fall back to an answer option.
export function questionPromptParts(text: string, word?: string): PromptPart[] {
  const target = word?.trim();
  if (!target) return [{ text, emphasized: false }];

  const literal = target.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const matches = text.matchAll(
    new RegExp(
      `(?<![\\p{L}\\p{M}\\p{N}_])${literal}(?![\\p{L}\\p{M}\\p{N}_])`,
      "giu",
    ),
  );
  const parts: PromptPart[] = [];
  let start = 0;
  for (const match of matches) {
    parts.push(
      { text: text.slice(start, match.index), emphasized: false },
      { text: match[0], emphasized: true },
    );
    start = match.index + match[0].length;
  }
  parts.push({ text: text.slice(start), emphasized: false });
  return parts;
}
