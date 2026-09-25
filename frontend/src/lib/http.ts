import { parseError, type APIError } from "../types/protocol";

export class ServiceError extends Error {
  constructor(readonly detail: APIError) {
    super(detail.message);
  }
}

export async function readResponse(
  response: Response,
  limit: number,
): Promise<Uint8Array<ArrayBuffer>> {
  const reader = response.body?.getReader();
  if (!reader) throw new Error("The service returned an empty response.");
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) {
        await reader.cancel();
        throw new Error("The service response was too large.");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const result = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return result;
}

export async function requestJSON<T>(
  path: string,
  parse: (value: unknown) => T,
  body?: unknown,
  signal?: AbortSignal,
  timeout = 6000,
  limit = 256 * 1024,
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.any([
      AbortSignal.timeout(timeout),
      ...(signal ? [signal] : []),
    ]),
  });
  if (
    !response.headers
      .get("Content-Type")
      ?.toLowerCase()
      .startsWith("application/json")
  ) {
    await response.body?.cancel();
    throw new Error("The service returned an unexpected response.");
  }
  const data: unknown = JSON.parse(
    new TextDecoder().decode(await readResponse(response, limit)),
  );
  if (!response.ok) throw new ServiceError(parseError(data));
  return parse(data);
}
