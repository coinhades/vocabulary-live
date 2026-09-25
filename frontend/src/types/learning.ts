export interface LearningScope {
  participantId: string;
  quizId: string;
  epoch: string;
  contentVersion: string;
}
export interface Capabilities {
  speechConfigured: boolean;
  textConfigured: boolean;
  promptVersion: string;
}
export interface Artifact<T> {
  artifactId: string;
  quizId: string;
  questionId: string;
  epoch: string;
  contentVersion: string;
  acceptedReceiptId: string;
  source: "ai" | "canonical";
  generatedAt: string;
  promptVersion: string;
  action: string;
  data: T;
}
export interface Explanation {
  explanation: string;
  contrast: string;
  example: string;
  canonicalMeaning: string;
  supported: boolean;
}
export interface Example {
  sentence: string;
  usageNote: string;
  canonicalMeaning: string;
  supported: boolean;
}
export interface Reflection {
  summary: string;
  suggestions: { questionId: string; step: string }[];
  supported: boolean;
}
export function parseReflection(v: unknown): Reflection {
  const d = object(v);
  return {
    summary: text(d.summary, 400),
    supported: bool(d.supported),
    suggestions: list(
      d.suggestions,
      (v) => {
        const s = object(v);
        return { questionId: text(s.questionId, 64), step: text(s.step, 240) };
      },
      3,
    ),
  };
}
export interface ReviewCard {
  id: string;
  questionId: string;
  prompt: string;
  options: { id: string; text: string }[];
  word: string;
  source: "ai" | "canonical";
  artifactId: string;
  reinforcement: boolean;
}
export interface ReviewFeedback {
  cardId: string;
  status: "checked" | "skipped";
  selectedOptionId: string;
  correct: boolean;
  correctOptionId: string;
  definition: string;
  explanation: string;
  hintUsed: boolean;
  needsAnotherLook: boolean;
}
export interface ReviewState {
  reviewSessionId: string;
  quizId: string;
  epoch: string;
  contentVersion: string;
  version: number;
  expiresAt: string;
  position: number;
  totalCards: number;
  completed: boolean;
  currentCard: ReviewCard | null;
  feedback: ReviewFeedback | null;
  hint: Artifact<{ hint: string; supported: boolean }> | null;
  checked: number;
  skipped: number;
  hintUsed: number;
  needsAnotherLook: number;
  fallback: string;
}
export function parseReview(v: unknown): ReviewState {
  const d = object(v);
  let card: ReviewCard | null = null,
    feedback: ReviewFeedback | null = null;
  if (d.currentCard !== null) {
    const c = object(d.currentCard);
    if (c.source !== "ai" && c.source !== "canonical")
      throw new Error("Invalid review source");
    const options = list(
      c.options,
      (v) => {
        const o = object(v);
        return { id: text(o.id, 64), text: text(o.text, 240) };
      },
      4,
    );
    if (options.length !== 4 || new Set(options.map((o) => o.id)).size !== 4)
      throw new Error("Invalid review choices");
    card = {
      id: text(c.id, 64),
      questionId: text(c.questionId, 64),
      prompt: text(c.prompt, 320),
      options,
      word: text(c.word, 80),
      source: c.source,
      artifactId: text(c.artifactId, 64),
      reinforcement: bool(c.reinforcement),
    };
  }
  if (d.feedback !== null) {
    const f = object(d.feedback);
    if (f.status !== "checked" && f.status !== "skipped")
      throw new Error("Invalid review status");
    feedback = {
      cardId: text(f.cardId, 64),
      status: f.status,
      selectedOptionId: text(f.selectedOptionId, 64),
      correct: bool(f.correct),
      correctOptionId: text(f.correctOptionId, 64),
      definition: text(f.definition, 600),
      explanation: text(f.explanation, 800),
      hintUsed: bool(f.hintUsed),
      needsAnotherLook: bool(f.needsAnotherLook),
    };
    if (
      feedback.cardId !== card?.id ||
      !card.options.some((o) => o.id === feedback!.correctOptionId)
    )
      throw new Error("Invalid review feedback");
  }
  const result = {
    reviewSessionId: text(d.reviewSessionId, 64),
    quizId: text(d.quizId, 64),
    epoch: text(d.epoch, 64),
    contentVersion: text(d.contentVersion, 128),
    version: integer(d.version),
    expiresAt: text(d.expiresAt, 64),
    position: integer(d.position, 7),
    totalCards: integer(d.totalCards, 6),
    completed: bool(d.completed),
    currentCard: card,
    feedback,
    hint:
      d.hint === null
        ? null
        : parseArtifact(d.hint, (v) => {
            const h = object(v);
            return { hint: text(h.hint, 200), supported: bool(h.supported) };
          }),
    checked: integer(d.checked, 6),
    skipped: integer(d.skipped, 6),
    hintUsed: integer(d.hintUsed, 6),
    needsAnotherLook: integer(d.needsAnotherLook, 6),
    fallback: text(d.fallback, 300),
  };
  if (
    result.completed !== (result.currentCard === null) ||
    result.totalCards < 1 ||
    result.position < 1 ||
    result.position > result.totalCards + 1
  )
    throw new Error("Invalid review progress");
  return result;
}
export function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw new Error("Invalid learning response");
  return v as Record<string, unknown>;
}
export function text(v: unknown, max = 2048): string {
  if (typeof v !== "string" || v.length > max)
    throw new Error("Invalid learning text");
  return v;
}
export function bool(v: unknown): boolean {
  if (typeof v !== "boolean") throw new Error("Invalid learning flag");
  return v;
}
export function integer(v: unknown, max = 1000): number {
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0 || v > max)
    throw new Error("Invalid learning number");
  return v;
}
export function list<T>(
  v: unknown,
  parse: (v: unknown) => T,
  max: number,
): T[] {
  if (!Array.isArray(v) || v.length > max)
    throw new Error("Invalid learning list");
  return v.map(parse);
}
export function parseCapabilities(value: unknown): Capabilities {
  const v = object(value);
  return {
    speechConfigured: bool(v.speechConfigured),
    textConfigured: bool(v.textConfigured),
    promptVersion: text(v.promptVersion, 80),
  };
}
export function parseArtifact<T>(
  value: unknown,
  parse: (v: unknown) => T,
): Artifact<T> {
  const v = object(value);
  if (v.source !== "ai" && v.source !== "canonical")
    throw new Error("Invalid provenance");
  return {
    artifactId: text(v.artifactId, 64),
    quizId: text(v.quizId, 64),
    questionId: text(v.questionId, 64),
    epoch: text(v.epoch, 64),
    contentVersion: text(v.contentVersion, 128),
    acceptedReceiptId: text(v.acceptedReceiptId, 64),
    source: v.source,
    generatedAt: text(v.generatedAt, 64),
    promptVersion: text(v.promptVersion, 80),
    action: text(v.action, 40),
    data: parse(v.data),
  };
}
export function parseExplanation(v: unknown): Explanation {
  const d = object(v);
  return {
    explanation: text(d.explanation, 600),
    contrast: text(d.contrast, 240),
    example: text(d.example, 240),
    canonicalMeaning: text(d.canonicalMeaning, 600),
    supported: bool(d.supported),
  };
}
export function parseExample(v: unknown): Example {
  const d = object(v);
  return {
    sentence: text(d.sentence, 280),
    usageNote: text(d.usageNote, 240),
    canonicalMeaning: text(d.canonicalMeaning, 600),
    supported: bool(d.supported),
  };
}
