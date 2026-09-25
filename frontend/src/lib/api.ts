import {
  parseAnswer,
  parseSession,
  parseState,
  parseMetadata,
  parseClock,
} from "../types/protocol";
import type { Answer } from "../types/protocol";
import { requestJSON as request } from "./http";
export { ServiceError } from "./http";
export const api = {
  start: (quizId: string, epoch: string, questionId: string) =>
    request(`/quizzes/${encodeURIComponent(quizId)}/start`, parseClock, {
      epoch,
      questionId,
    }),
  preview: (quizId: string, signal?: AbortSignal) =>
    request(
      `/quizzes/${encodeURIComponent(quizId)}`,
      parseMetadata,
      undefined,
      signal,
    ),
  session: (restart = false, resumeOnly = false) =>
    request(
      "/session",
      parseSession,
      restart ? { restart: true } : resumeOnly ? { resumeOnly: true } : {},
    ),
  join: (quizId: string, displayName: string) =>
    request(`/quizzes/${encodeURIComponent(quizId)}/join`, parseState, {
      displayName,
    }),
  state: (quizId: string) =>
    request(`/quizzes/${encodeURIComponent(quizId)}/state`, parseState),
  answer: (quizId: string, answer: Answer) =>
    request(
      `/quizzes/${encodeURIComponent(quizId)}/answers`,
      parseAnswer,
      answer,
    ),
};
