import { bool, integer, list, object, text } from "./learning";
export interface Brief {
  topic: string;
  context: string;
  level: string;
  count: number;
  targetWords: string[];
}
export interface ItemContent {
  word: string;
  sense: string;
  partOfSpeech: string;
  pronunciation: string;
  meaning: string;
  prompt: string;
  options: string[];
  correctIndex: number;
  explanation: string;
  example: string;
  difficulty: string;
  level: string;
  tags: string[];
  suggestedTags: string[];
  pronunciationVisible: boolean;
}
export interface Approval {
  contentHash: string;
  draftVersion: number;
  reviewedAt: string;
}
export interface DraftItem {
  id: string;
  content: ItemContent;
  approval: Approval | null;
}
export interface DraftSummary {
  id: string;
  title: string;
  status: "needs-review" | "published";
  version: number;
  publishedQuizId: string;
  expiresAt: string;
}
export interface Draft extends DraftSummary {
  brief: Brief;
  items: DraftItem[];
  contentVersion: string;
  createdAt: string;
}
export interface Preview {
  previewId: string;
  draftId: string;
  questionId: string;
  baseVersion: number;
  source: "ai";
  content: ItemContent;
}
function enumValue(v: unknown, allowed: string[]): string {
  const s = text(v, 40);
  if (!allowed.includes(s)) throw new Error("Invalid authoring value");
  return s;
}
export const levels = ["A1", "A2", "B1", "B2", "C1", "C2"];
export const contexts = ["daily-life", "work", "travel", "school"];
export function parseContent(v: unknown): ItemContent {
  const d = object(v),
    options = list(d.options, (v) => text(v, 240), 4);
  if (options.length !== 4) throw new Error("Four options required");
  return {
    word: text(d.word, 80),
    sense: text(d.sense, 200),
    partOfSpeech: enumValue(d.partOfSpeech, [
      "noun",
      "verb",
      "adjective",
      "adverb",
    ]),
    pronunciation: text(d.pronunciation, 100),
    meaning: text(d.meaning, 600),
    prompt: text(d.prompt, 320),
    options,
    correctIndex: integer(d.correctIndex, 3),
    explanation: text(d.explanation, 800),
    example: text(d.example, 280),
    difficulty: enumValue(d.difficulty, ["easy", "medium", "hard"]),
    level: enumValue(d.level, levels),
    tags: list(d.tags, (v) => text(v, 40), 5),
    suggestedTags: list(d.suggestedTags, (v) => text(v, 40), 5),
    pronunciationVisible: bool(d.pronunciationVisible),
  };
}
export function parseSummary(v: unknown): DraftSummary {
  const d = object(v);
  if (d.status !== "needs-review" && d.status !== "published")
    throw new Error("Invalid draft status");
  return {
    id: text(d.id, 32),
    title: text(d.title, 100),
    status: d.status,
    version: integer(d.version, 10000),
    publishedQuizId: text(d.publishedQuizId, 64),
    expiresAt: text(d.expiresAt, 64),
  };
}
export function parseDraft(v: unknown): Draft {
  const d = object(v),
    b = object(d.brief),
    count = integer(b.count, 8);
  if (count < 4) throw new Error("Invalid question count");
  const items = list(
    d.items,
    (v) => {
      const item = object(v);
      let approval: Approval | null = null;
      if (item.approval !== null) {
        const a = object(item.approval);
        approval = {
          contentHash: text(a.contentHash, 64),
          draftVersion: integer(a.draftVersion, 10000),
          reviewedAt: text(a.reviewedAt, 64),
        };
      }
      return {
        id: text(item.id, 32),
        content: parseContent(item.content),
        approval,
      };
    },
    8,
  );
  if (items.length !== count) throw new Error("Invalid draft items");
  return {
    ...parseSummary(d),
    brief: {
      topic: text(b.topic, 200),
      context: enumValue(b.context, contexts),
      level: enumValue(b.level, levels),
      count,
      targetWords: list(b.targetWords ?? [], (v) => text(v, 80), 8),
    },
    items,
    contentVersion: text(d.contentVersion, 128),
    createdAt: text(d.createdAt, 64),
  };
}
export function parsePreview(v: unknown): Preview {
  const d = object(v);
  if (d.source !== "ai") throw new Error("Invalid preview source");
  return {
    previewId: text(d.previewId, 64),
    draftId: text(d.draftId, 32),
    questionId: text(d.questionId, 32),
    baseVersion: integer(d.baseVersion, 10000),
    source: d.source,
    content: parseContent(d.content),
  };
}
export function parseOperator(v: unknown) {
  const d = object(v);
  if (d.authenticated !== true) throw new Error("Not authenticated");
  return { authenticated: true, textConfigured: bool(d.textConfigured) };
}
