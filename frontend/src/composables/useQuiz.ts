import { markRaw, onUnmounted, reactive } from "vue";
import { api } from "../lib/api";
import { QuizMachine } from "../lib/quiz-machine";
import { sessionPersistence } from "../lib/storage";
import type { Socket } from "../lib/quiz-machine";

export function useQuiz() {
  const quiz = reactive(
    new QuizMachine(
      markRaw({
        transport: api,
        storage: sessionPersistence,
        uuid: () => crypto.randomUUID(),
        now: () => performance.now(),
        random: () => Math.random(),
        socket: (id: string) => {
          const ws = new WebSocket(
            `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/quizzes/${encodeURIComponent(id)}/live`,
          );
          const socket: Socket = markRaw({
            onopen: null,
            onmessage: null,
            onclose: null,
            onerror: null,
            close: () => ws.close(),
          });
          ws.onopen = () => socket.onopen?.();
          ws.onmessage = (event: MessageEvent<string>) =>
            socket.onmessage?.({ data: event.data });
          ws.onclose = () => socket.onclose?.();
          ws.onerror = () => socket.onerror?.();
          return socket;
        },
      }),
    ),
  );
  onUnmounted(() => quiz.stop());
  return quiz;
}
