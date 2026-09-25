import type { Receipt } from "../types/protocol";

// Use accepted private feedback, never duplicate grading data in the bundle.
export function learningWord(receipt: Receipt): string {
  return receipt.vocabulary.word;
}

export function learningDefinition(receipt: Receipt): string {
  return receipt.vocabulary.definition;
}
