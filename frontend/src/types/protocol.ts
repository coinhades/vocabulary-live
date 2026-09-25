export interface Option {
  id: string;
  text: string;
}
export interface Question {
  id: string;
  prompt: string;
  options: Option[];
  timing: QuestionTiming;
  pronunciationText?: string;
}
export interface QuestionTiming {
  difficulty: "easy" | "medium" | "hard";
  durationSeconds: number;
}
export interface QuestionClock {
  epoch: string;
  questionId: string;
  deadlineMs: number;
  serverTimeMs: number;
}
export interface Vocabulary {
  word: string;
  pronunciation: string;
  partOfSpeech: "noun" | "verb" | "adjective" | "adverb";
  definition: string;
  example?: string;
}
export interface Row {
  participantId: string;
  displayName: string;
  score: number;
  rank: number;
  answeredCount: number;
}
export interface Snapshot {
  quizId: string;
  epoch: string;
  version: number;
  participantCount: number;
  leaderboard: Row[];
}
export interface Answer {
  submissionId: string;
  epoch: string;
  questionId: string;
  optionId: string;
}
export interface Pending extends Answer {
  quizId: string;
  participantId: string;
}
export interface Receipt {
  submissionId: string;
  quizId: string;
  epoch: string;
  questionId: string;
  selectedOptionId: string;
  correctness: boolean;
  timedOut: boolean;
  pointsAwarded: number;
  totalScoreAtAcceptance: number;
  acceptedVersion: number;
  correctOptionId: string;
  explanation: string;
  question: Question;
  questionNumber: number;
  vocabulary: Vocabulary;
}
export interface Policies {
  mode: "LIVE_PRACTICE";
  feedbackPolicy: "IMMEDIATE";
  questionOrderPolicy: "PER_PARTICIPANT_SHUFFLED";
  answerOrderPolicy: "PER_PARTICIPANT_SHUFFLED";
  leaderboardPolicy: "LIVE_ALL";
  scoringPolicy: "FIRST_ANSWER_TIMED_100_50";
}
export interface Metadata {
  title: string;
  contentVersion: string;
  totalQuestions: number;
  pointsPerCorrectAnswer: number;
  pointsAfterTimeout: number;
  policies: Policies;
}
export interface State extends Snapshot, Metadata {
  participantId: string;
  answeredCount: number;
  score: number;
  completed: boolean;
  correctCount: number;
  accuracy: number;
  currentRank: number;
  questionNumber: number;
  currentQuestion: Question | null;
  receipts: Receipt[];
}
export interface AnswerResult {
  outcome: "accepted" | "replayed";
  receipt: Receipt;
}
export type LiveEvent =
  { type: "snapshot"; snapshot: Snapshot } | { type: "status"; code: string };
export interface APIError {
  code: string;
  message: string;
  requestId: string;
  retryable: boolean;
  receipt?: Receipt;
}

// Decode at the network boundary; TypeScript annotations alone do not validate JSON.
function object(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== "object" || Array.isArray(v))
    throw new Error("Invalid response object");
  return v as Record<string, unknown>;
}
function text(v: unknown): string {
  if (typeof v !== "string" || v.length > 2048)
    throw new Error("Invalid response text");
  return v;
}
function integer(v: unknown, max = Number.MAX_SAFE_INTEGER): number {
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0 || v > max)
    throw new Error("Invalid response number");
  return v;
}
function boolean(v: unknown): boolean {
  if (typeof v !== "boolean") throw new Error("Invalid response boolean");
  return v;
}
function list<T>(v: unknown, decode: (value: unknown) => T, max: number): T[] {
  if (!Array.isArray(v) || v.length > max)
    throw new Error("Invalid response list");
  return v.map(decode);
}
export function parseReceipt(value: unknown): Receipt {
  const v = object(value);
  const receipt = {
    submissionId: text(v.submissionId),
    quizId: text(v.quizId),
    epoch: text(v.epoch),
    questionId: text(v.questionId),
    selectedOptionId: text(v.selectedOptionId),
    correctness: boolean(v.correctness),
    timedOut: boolean(v.timedOut),
    pointsAwarded: integer(v.pointsAwarded, 100),
    totalScoreAtAcceptance: integer(v.totalScoreAtAcceptance, 800),
    acceptedVersion: integer(v.acceptedVersion),
    correctOptionId: text(v.correctOptionId),
    explanation: text(v.explanation),
    question: parseQuestion(v.question),
    questionNumber: integer(v.questionNumber, 8),
    vocabulary: parseVocabulary(v.vocabulary),
  };
  if (
    receipt.pointsAwarded !==
      (receipt.correctness ? (receipt.timedOut ? 50 : 100) : 0) ||
    receipt.totalScoreAtAcceptance % 50 !== 0 ||
    receipt.acceptedVersion < 1 ||
    receipt.correctness !==
      (receipt.selectedOptionId === receipt.correctOptionId) ||
    receipt.questionNumber < 1 ||
    receipt.question.id !== receipt.questionId ||
    !receipt.question.options.some((o) => o.id === receipt.selectedOptionId) ||
    !receipt.question.options.some((o) => o.id === receipt.correctOptionId)
  )
    throw new Error("Inconsistent receipt");
  return receipt;
}
export function parseSnapshot(value: unknown): Snapshot {
  const v = object(value);
  const leaderboard = list(
    v.leaderboard,
    (value) => {
      const r = object(value);
      return {
        participantId: text(r.participantId),
        displayName: text(r.displayName),
        score: integer(r.score, 800),
        rank: integer(r.rank, 200),
        answeredCount: integer(r.answeredCount, 8),
      };
    },
    200,
  );
  const count = integer(v.participantCount, 200);
  if (
    count !== leaderboard.length ||
    new Set(leaderboard.map((r) => r.participantId)).size !== count
  )
    throw new Error("Invalid participant count");
  let rank = 0;
  for (let i = 0; i < leaderboard.length; i++) {
    const row = leaderboard[i];
    const previous = leaderboard[i - 1];
    if (!previous || row.score !== previous.score) rank = i + 1;
    if (
      row.rank !== rank ||
      row.score % 50 !== 0 ||
      row.score > row.answeredCount * 100 ||
      (previous && row.score > previous.score)
    )
      throw new Error("Inconsistent standings");
  }
  return {
    quizId: text(v.quizId),
    epoch: text(v.epoch),
    version: integer(v.version),
    participantCount: count,
    leaderboard,
  };
}
function parseQuestion(value: unknown): Question {
  const q = object(value);
  const options = list(
    q.options,
    (value) => {
      const o = object(value);
      return { id: text(o.id), text: text(o.text) };
    },
    4,
  );
  if (options.length !== 4 || new Set(options.map((o) => o.id)).size !== 4)
    throw new Error("Invalid options");
  const timing = object(q.timing);
  const difficulty = timing.difficulty;
  if (difficulty !== "easy" && difficulty !== "medium" && difficulty !== "hard")
    throw new Error("Invalid word difficulty");
  const durationSeconds = integer(timing.durationSeconds, 120);
  if (durationSeconds < 1) throw new Error("Invalid countdown duration");
  return {
    id: text(q.id),
    prompt: text(q.prompt),
    options,
    timing: { difficulty, durationSeconds },
    ...(q.pronunciationText === undefined
      ? {}
      : { pronunciationText: text(q.pronunciationText) }),
  };
}

function parseVocabulary(value: unknown): Vocabulary {
  const v = object(value);
  const partOfSpeech = v.partOfSpeech;
  if (
    partOfSpeech !== "noun" &&
    partOfSpeech !== "verb" &&
    partOfSpeech !== "adjective" &&
    partOfSpeech !== "adverb"
  )
    throw new Error("Invalid part of speech");
  return {
    word: text(v.word),
    pronunciation: text(v.pronunciation),
    definition: text(v.definition),
    ...(v.example === undefined ? {} : { example: text(v.example) }),
    partOfSpeech,
  };
}

export function parseMetadata(value: unknown): Metadata {
  const v = object(value);
  const p = object(v.policies);
  if (
    p.mode !== "LIVE_PRACTICE" ||
    p.feedbackPolicy !== "IMMEDIATE" ||
    p.questionOrderPolicy !== "PER_PARTICIPANT_SHUFFLED" ||
    p.answerOrderPolicy !== "PER_PARTICIPANT_SHUFFLED" ||
    p.leaderboardPolicy !== "LIVE_ALL" ||
    p.scoringPolicy !== "FIRST_ANSWER_TIMED_100_50" ||
    typeof v.totalQuestions !== "number" ||
    !Number.isInteger(v.totalQuestions) ||
    v.totalQuestions < 4 ||
    v.totalQuestions > 8 ||
    v.pointsPerCorrectAnswer !== 100 ||
    v.pointsAfterTimeout !== 50
  )
    throw new Error("Unsupported quiz policies");
  return {
    title: text(v.title),
    contentVersion: text(v.contentVersion),
    totalQuestions: v.totalQuestions,
    pointsPerCorrectAnswer: 100,
    pointsAfterTimeout: 50,
    policies: {
      mode: p.mode,
      feedbackPolicy: p.feedbackPolicy,
      questionOrderPolicy: p.questionOrderPolicy,
      answerOrderPolicy: p.answerOrderPolicy,
      leaderboardPolicy: p.leaderboardPolicy,
      scoringPolicy: p.scoringPolicy,
    },
  };
}

export function parseState(value: unknown): State {
  const v = object(value);
  if (
    typeof v.accuracy !== "number" ||
    !Number.isFinite(v.accuracy) ||
    v.accuracy < 0 ||
    v.accuracy > 100
  )
    throw new Error("Invalid accuracy");
  const state = {
    ...parseSnapshot(v),
    ...parseMetadata(v),
    participantId: text(v.participantId),
    answeredCount: integer(v.answeredCount, 8),
    score: integer(v.score, 800),
    completed: boolean(v.completed),
    correctCount: integer(v.correctCount, 8),
    accuracy: v.accuracy,
    currentRank: integer(v.currentRank, 200),
    questionNumber: integer(v.questionNumber, 8),
    currentQuestion:
      v.currentQuestion === null ? null : parseQuestion(v.currentQuestion),
    receipts: list(v.receipts, parseReceipt, 8),
  };
  if (
    state.answeredCount > state.totalQuestions ||
    state.questionNumber > state.totalQuestions ||
    state.receipts.some((r) => r.questionNumber > state.totalQuestions) ||
    state.leaderboard.some((r) => r.answeredCount > state.totalQuestions) ||
    state.correctCount !== state.receipts.filter((r) => r.correctness).length ||
    state.accuracy !==
      Math.round((state.correctCount / state.totalQuestions) * 1000) / 10 ||
    !state.leaderboard.some((r) => r.participantId === state.participantId) ||
    state.completed !== (state.answeredCount === state.totalQuestions) ||
    state.completed !== (state.currentQuestion === null) ||
    (state.completed ? state.questionNumber !== 0 : state.questionNumber < 1)
  )
    throw new Error("Invalid private state");
  const me = state.leaderboard.find(
    (r) => r.participantId === state.participantId,
  );
  if (
    new Set(state.receipts.map((r) => r.questionId)).size !==
      state.receipts.length ||
    new Set(state.receipts.map((r) => r.questionNumber)).size !==
      state.receipts.length ||
    me?.answeredCount !== state.receipts.length ||
    me.answeredCount !== state.answeredCount ||
    me.score !== state.score ||
    me.rank !== state.currentRank ||
    me.score !==
      state.receipts.reduce((total, r) => total + r.pointsAwarded, 0) ||
    state.receipts.some(
      (r) =>
        r.questionId === state.currentQuestion?.id ||
        r.questionNumber === state.questionNumber,
    )
  )
    throw new Error("Inconsistent private progress");
  let acceptedVersion = 0;
  let totalScore = 0;
  for (const receipt of state.receipts) {
    totalScore += receipt.pointsAwarded;
    if (
      receipt.quizId !== state.quizId ||
      receipt.epoch !== state.epoch ||
      receipt.acceptedVersion > state.version ||
      receipt.acceptedVersion <= acceptedVersion ||
      receipt.totalScoreAtAcceptance !== totalScore
    )
      throw new Error("Receipt does not belong to this state");
    acceptedVersion = receipt.acceptedVersion;
  }
  return state;
}
export function parseAnswer(value: unknown): AnswerResult {
  const v = object(value);
  if (v.outcome !== "accepted" && v.outcome !== "replayed")
    throw new Error("Invalid answer outcome");
  return { outcome: v.outcome, receipt: parseReceipt(v.receipt) };
}
export function parseEvent(value: unknown): LiveEvent {
  const v = object(value);
  if (v.type === "snapshot")
    return { type: "snapshot", snapshot: parseSnapshot(v.snapshot) };
  if (v.type === "status") return { type: "status", code: text(v.code) };
  throw new Error("Unknown event type");
}
export function parseError(value: unknown): APIError {
  const v = object(object(value).error);
  return {
    code: text(v.code),
    message: text(v.message),
    requestId: text(v.requestId),
    retryable: boolean(v.retryable),
    receipt: v.receipt ? parseReceipt(v.receipt) : undefined,
  };
}
export function parseSession(value: unknown): string {
  return text(object(value).participantId);
}
export function parseClock(value: unknown): QuestionClock {
  const v = object(value);
  const clock = {
    epoch: text(v.epoch),
    questionId: text(v.questionId),
    deadlineMs: integer(v.deadlineMs),
    serverTimeMs: integer(v.serverTimeMs),
  };
  if (
    !clock.deadlineMs ||
    !clock.serverTimeMs ||
    clock.deadlineMs - clock.serverTimeMs > 120000
  )
    throw new Error("Invalid server clock");
  return clock;
}
export function parsePending(value: unknown): Pending {
  const v = object(value);
  return {
    quizId: text(v.quizId),
    participantId: text(v.participantId),
    submissionId: text(v.submissionId),
    epoch: text(v.epoch),
    questionId: text(v.questionId),
    optionId: text(v.optionId),
  };
}
