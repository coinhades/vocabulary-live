import { parseError } from "../types/protocol";
import { ServiceError, requestJSON, readResponse } from "./http";
const credentialListeners = new Set<() => void>();
export function onLearningCredentialsRejected(listener: () => void) {
  credentialListeners.add(listener);
  return () => credentialListeners.delete(listener);
}
function serviceError(data: unknown) {
  const error = new ServiceError(parseError(data));
  if (error.detail.code === "AI_CREDENTIALS_INVALID")
    for (const listener of credentialListeners) listener();
  return error;
}
export async function learningRequest<T>(
  path: string,
  parse: (v: unknown) => T,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  try {
    return await requestJSON(path, parse, body, signal, 52000, 131072);
  } catch (error) {
    if (
      error instanceof ServiceError &&
      error.detail.code === "AI_CREDENTIALS_INVALID"
    )
      for (const listener of credentialListeners) listener();
    throw error;
  }
}
export async function learningAudio(
  path: string,
  epoch: string,
  signal: AbortSignal,
): Promise<Blob> {
  const response = await fetch(`/api${path}`, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ epoch }),
    signal: AbortSignal.any([signal, AbortSignal.timeout(22000)]),
  });
  if (!response.ok)
    throw serviceError(
      JSON.parse(
        new TextDecoder().decode(await readResponse(response, 131072)),
      ),
    );
  if (!response.headers.get("Content-Type")?.startsWith("audio/mpeg")) {
    await response.body?.cancel();
    throw new Error("This audio could not be played.");
  }
  const audio = await readResponse(response, 1048576);
  if (!audio.byteLength) throw new Error("This audio could not be played.");
  return new Blob([audio], { type: "audio/mpeg" });
}
export function learningError(e: unknown): string {
  return e instanceof ServiceError
    ? e.message
    : e instanceof DOMException && e.name === "TimeoutError"
      ? "This request took too long. You can continue or retry."
      : "This learning request could not finish. You can continue and try again.";
}
