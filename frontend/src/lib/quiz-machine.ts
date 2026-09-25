import { ServiceError } from "./api";
import { StorageUnavailableError } from "./storage";
import { parseEvent, parsePending } from "../types/protocol";
import type {
  Answer,
  AnswerResult,
  Pending,
  Receipt,
  Snapshot,
  State,
  QuestionClock,
} from "../types/protocol";

export type Connection =
  | "idle"
  | "connecting"
  | "synchronizing"
  | "live"
  | "reconnecting"
  | "stale"
  | "unavailable"
  | "expired"
  | "reset";
export type Action =
  "READY" | "SELECTED" | "SUBMITTING" | "FEEDBACK" | "COMPLETED";
export interface Transport {
  start(id: string, epoch: string, questionId: string): Promise<QuestionClock>;
  session(restart?: boolean, resumeOnly?: boolean): Promise<string>;
  join(id: string, name: string): Promise<State>;
  state(id: string): Promise<State>;
  answer(id: string, answer: Answer): Promise<AnswerResult>;
}
export interface Socket {
  onopen: (() => void) | null;
  onmessage: ((e: { data: string }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  close(): void;
}
export interface Dependencies {
  transport: Transport;
  storage: Pick<Storage, "getItem" | "setItem" | "removeItem">;
  socket: (id: string) => Socket;
  uuid: () => string;
  now: () => number;
  random: () => number;
}

// One owner for request generations, socket lifetimes and persistent submission
// intent. Receipts are historical; only snapshots supply the current score.
export class QuizMachine {
  state: State | null = null;
  quizId = "";
  displayName = "";
  connection: Connection = "idle";
  submission: "idle" | "checking" | "unconfirmed" = "idle";
  message = "";
  networkMessage = "";
  busy = false;
  pending: Pending | null = null;
  selected = "";
  feedback: Receipt | null = null;
  clock: QuestionClock | null = null;
  clockReceivedAt = 0;
  clockMessage = "";
  private starting: string | null = null;
  private generation = 0;
  private requestSequence = 0;
  private appliedSequence = 0;
  private socketSequence = 0;
  private ws: Socket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private freshnessTimer: ReturnType<typeof setInterval> | undefined;
  private attempts = 0;
  private lastSynchronized = 0;
  private standingsInterrupted = false;
  private resolving: Promise<void> | null = null;
  private privateVersion = 0;
  private dismissedVersion = 0;
  private sending: Pending | null = null;
  private publicSync: Promise<void> | null = null;

  constructor(private readonly deps: Dependencies) {}
  get me() {
    return this.state?.leaderboard.find(
      (r) => r.participantId === this.state?.participantId,
    );
  }
  get question() {
    return this.feedback?.question ?? this.state?.currentQuestion;
  }
  get questionNumber() {
    return this.feedback?.questionNumber ?? this.state?.questionNumber ?? 0;
  }
  get complete() {
    return !!this.state?.completed && !this.feedback && !this.pending;
  }
  get action(): Action {
    if (this.feedback) return "FEEDBACK";
    if (this.pending) return "SUBMITTING";
    if (this.complete) return "COMPLETED";
    return this.selected ? "SELECTED" : "READY";
  }
  get canSelect() {
    return (
      !!this.question &&
      !this.pending &&
      !this.feedback &&
      !this.busy &&
      !this.needsRecovery() &&
      this.clock?.questionId === this.question?.id &&
      this.clock?.epoch === this.state?.epoch &&
      this.me?.answeredCount === this.state?.answeredCount
    );
  }
  get canAdvance() {
    return (
      !!this.feedback &&
      !this.pending &&
      !this.needsRecovery() &&
      this.privateVersion >= this.feedback.acceptedVersion &&
      !!this.state?.receipts.some(
        (r) => r.questionId === this.feedback?.questionId,
      )
    );
  }
  get missed() {
    return this.state?.receipts.filter((r) => !r.correctness) ?? [];
  }
  get correctCount() {
    return this.state?.receipts.filter((r) => r.correctness).length ?? 0;
  }
  get networkNotice() {
    if (!this.state || this.needsRecovery()) return "";
    if (this.clockMessage && !this.feedback && !this.pending && !this.complete)
      return this.clockMessage;
    if (this.networkMessage) return this.networkMessage;
    // Connecting and synchronizing are normal activity, not evidence of a
    // failure. Once interrupted, keep the warning through retry handshakes
    // until a verified snapshot confirms recovery.
    if (this.standingsInterrupted)
      return "Standings are reconnecting. Showing last known scores.";
    if (this.connection === "unavailable")
      return "Service unavailable. Your progress is saved; standings show the last known scores.";
    return "";
  }
  private key(id = this.quizId) {
    return `vocabulary.pending.${id}`;
  }
  private alive(g: number) {
    return g === this.generation;
  }
  private needsRecovery() {
    return this.connection === "expired" || this.connection === "reset";
  }

  async join(id: string, name: string) {
    this.stop();
    const g = this.generation;
    this.quizId = id.trim().toUpperCase();
    this.displayName = name.trim();
    this.state = null;
    this.message = "";
    this.networkMessage = "";
    this.busy = true;
    this.submission = "idle";
    this.connection = "connecting";
    this.pending = null;
    this.feedback = null;
    this.selected = "";
    this.clock = null;
    this.clockMessage = "";
    this.privateVersion = 0;
    this.dismissedVersion = 0;
    try {
      const previousIdentity = this.deps.storage.getItem("vocabulary.identity");
      const identity = await this.deps.transport.session(
        false,
        !!previousIdentity,
      );
      if (!this.alive(g)) return;
      if (previousIdentity && previousIdentity !== identity) {
        this.connection = "expired";
        this.message =
          "This browser's anonymous identity changed. Start a new session explicitly to continue; your previous score stays in the standings.";
        return;
      }
      this.deps.storage.setItem("vocabulary.identity", identity);
      const state = await this.deps.transport.join(
        this.quizId,
        this.displayName,
      );
      if (!this.alive(g)) return;
      this.applyPrivate(state, ++this.requestSequence);
      this.loadPending();
      try {
        this.deps.storage.setItem(
          "vocabulary.last",
          JSON.stringify({
            quizId: this.quizId,
            displayName: this.displayName,
          }),
        );
      } catch {
        this.message =
          "Automatic resumption is unavailable. Keep this page open, or rejoin with the same quiz code.";
      }
      await this.resolvePending(g);
      await this.startCurrentQuestion(g);
      if (!this.alive(g) || this.needsRecovery()) return;
      this.lastSynchronized = this.deps.now();
      this.connect(g);
      this.freshnessTimer = setInterval(() => {
        if (
          ["live", "connecting", "synchronizing"].includes(this.connection) &&
          this.deps.now() - this.lastSynchronized > 5500
        ) {
          this.connection = "stale";
          this.scheduleReconnect(g);
        }
      }, 1000);
    } catch (error) {
      if (this.alive(g)) this.fail(error);
    } finally {
      if (this.alive(g)) this.busy = false;
    }
  }

  private loadPending() {
    const raw = this.deps.storage.getItem(this.key());
    if (!raw) return;
    try {
      const pending = parsePending(JSON.parse(raw));
      if (
        pending.quizId !== this.quizId ||
        pending.participantId !== this.state?.participantId ||
        pending.epoch !== this.state.epoch
      ) {
        this.clearPending();
        this.message =
          "A previous pending answer belongs to an earlier session or quiz reset. It was not replayed.";
        return;
      }
      this.pending = pending;
      this.feedback = null;
      this.selected = pending.optionId;
      this.submission = "unconfirmed";
    } catch {
      this.connection = "stale";
      this.message =
        "Saved answer data could not be read. Rejoin to synchronize.";
    }
  }

  private applyPrivate(incoming: State, sequence: number) {
    if (incoming.quizId !== this.quizId || sequence < this.appliedSequence)
      return;
    const previous = this.state;
    if (
      previous?.epoch === incoming.epoch &&
      incoming.version < this.privateVersion
    )
      return;
    this.appliedSequence = sequence;
    this.privateVersion = incoming.version;
    const previousQuestion = this.question?.id;
    if (
      previous?.epoch === incoming.epoch &&
      previous.version > incoming.version
    ) {
      incoming = {
        ...incoming,
        version: previous.version,
        participantCount: previous.participantCount,
        leaderboard: previous.leaderboard,
      };
    }
    this.state = incoming;
    if (
      this.pending &&
      (this.pending.epoch !== incoming.epoch ||
        this.pending.participantId !== incoming.participantId)
    ) {
      this.clearPending();
      this.message =
        "The quiz was reset. The previous pending answer was not replayed.";
    }
    if (!previous || previous.epoch !== incoming.epoch) {
      this.feedback = null;
      this.selected = "";
      this.dismissedVersion = this.readDismissed();
    }
    const pendingReceipt = incoming.receipts.find(
      (r) => r.questionId === this.pending?.questionId,
    );
    if (pendingReceipt) this.accept(pendingReceipt);
    if (!this.feedback && !this.pending) {
      const latest = incoming.receipts.at(-1);
      if (latest && latest.acceptedVersion > this.dismissedVersion)
        this.showFeedback(latest);
      else if (previousQuestion !== this.question?.id) this.selected = "";
    }
  }

  private readDismissed() {
    try {
      const raw = this.deps.storage.getItem(
        `vocabulary.feedback.${this.quizId}`,
      );
      if (!raw) return 0;
      const value: unknown = JSON.parse(raw);
      if (
        value &&
        typeof value === "object" &&
        "epoch" in value &&
        "participantId" in value &&
        "version" in value &&
        value.epoch === this.state?.epoch &&
        value.participantId === this.state?.participantId &&
        typeof value.version === "number" &&
        Number.isSafeInteger(value.version) &&
        value.version >= 0
      )
        return value.version;
    } catch {
      /* Feedback stays visible if its dismissal cannot be restored. */
    }
    return 0;
  }

  private showFeedback(receipt: Receipt) {
    this.feedback = receipt;
    this.selected = receipt.selectedOptionId;
    this.submission = "idle";
  }

  receive(snapshot: Snapshot) {
    if (!this.state || snapshot.quizId !== this.quizId) return;
    if (snapshot.epoch !== this.state.epoch) {
      // A different epoch on a socket is only a reason to ask the authority.
      // Never let an old delayed socket message roll state back to an old epoch.
      this.synchronizePublic();
      return;
    }
    if (snapshot.version < this.state.version) return;
    // A public update can replace only public fields. Private cursor/receipts
    // must come from the authenticated state endpoint, including during recovery.
    if (snapshot.version > this.state.version) {
      this.state = {
        ...this.state,
        version: snapshot.version,
        participantCount: snapshot.participantCount,
        leaderboard: snapshot.leaderboard,
      };
    }
    this.lastSynchronized = this.deps.now();
    this.attempts = 0;
    const me = snapshot.leaderboard.find(
      (r) => r.participantId === this.state?.participantId,
    );
    if (me && me.answeredCount > this.state.answeredCount) {
      this.connection = "synchronizing";
      this.synchronizePublic();
      return;
    }
    this.connection = "live";
    this.standingsInterrupted = false;
    this.networkMessage = "";
  }

  private synchronizePublic() {
    if (this.publicSync) return;
    const request = this.synchronize(this.generation);
    this.publicSync = request;
    void request.finally(() => {
      if (this.publicSync === request) this.publicSync = null;
    });
  }

  private async synchronize(g: number) {
    const sequence = ++this.requestSequence;
    try {
      const state = await this.deps.transport.state(this.quizId);
      if (!this.alive(g)) return;
      this.applyPrivate(state, sequence);
      this.networkMessage = "";
      await this.resolvePending(g);
      await this.startCurrentQuestion(g);
    } catch (error) {
      if (this.alive(g)) this.fail(error);
    }
  }

  private async startCurrentQuestion(g: number) {
    if (
      !this.alive(g) ||
      !this.state ||
      !this.state.currentQuestion ||
      this.feedback ||
      this.pending ||
      this.needsRecovery()
    )
      return;
    const { epoch, currentQuestion } = this.state;
    if (
      this.clock?.epoch === epoch &&
      this.clock.questionId === currentQuestion.id
    )
      return;
    const key = `${g}.${epoch}.${currentQuestion.id}`;
    if (this.starting === key) return;
    this.starting = key;
    try {
      const clock = await this.deps.transport.start(
        this.quizId,
        epoch,
        currentQuestion.id,
      );
      if (
        !this.alive(g) ||
        this.state?.epoch !== epoch ||
        this.state.currentQuestion?.id !== currentQuestion.id ||
        this.feedback
      )
        return;
      if (clock.epoch !== epoch || clock.questionId !== currentQuestion.id)
        throw new Error("Clock does not belong to this question");
      this.clock = clock;
      this.clockReceivedAt = this.deps.now();
      this.clockMessage = "";
    } catch (error) {
      if (
        !this.alive(g) ||
        this.state?.epoch !== epoch ||
        this.state.currentQuestion?.id !== currentQuestion.id ||
        this.feedback
      )
        return;
      this.clockMessage =
        "The question timer could not be synchronized. Retry to continue.";
      if (
        error instanceof ServiceError &&
        error.detail.code === "QUESTION_ALREADY_ANSWERED"
      ) {
        try {
          const state = await this.deps.transport.state(this.quizId);
          if (this.alive(g)) this.applyPrivate(state, ++this.requestSequence);
        } catch (failure) {
          if (this.alive(g)) this.fail(failure);
        }
      } else this.fail(error);
    } finally {
      if (this.starting === key) this.starting = null;
    }
  }

  async submit() {
    if (
      !this.state ||
      !this.question ||
      !this.selected ||
      this.pending ||
      this.feedback ||
      !this.canSelect
    )
      return;
    const pending: Pending = {
      quizId: this.quizId,
      participantId: this.state.participantId,
      epoch: this.state.epoch,
      questionId: this.question.id,
      optionId: this.selected,
      submissionId: this.deps.uuid(),
    };
    // Persist before sending so reload can safely resolve an ambiguous result.
    try {
      this.deps.storage.setItem(this.key(), JSON.stringify(pending));
    } catch {
      this.message =
        "Browser storage is unavailable. Enable site storage before checking so your answer can be recovered safely.";
      return;
    }
    this.pending = pending;
    await this.sendPending(this.generation);
  }

  private async sendPending(g: number) {
    const pending = this.pending;
    if (
      !pending ||
      !this.state ||
      pending.epoch !== this.state.epoch ||
      this.sending === pending
    )
      return;
    this.sending = pending;
    this.submission = "checking";
    this.message = "";
    try {
      const { submissionId, epoch, questionId, optionId } = pending;
      const result = await this.deps.transport.answer(pending.quizId, {
        submissionId,
        epoch,
        questionId,
        optionId,
      });
      if (!this.alive(g) || this.pending?.submissionId !== submissionId) return;
      this.accept(result.receipt);
      if (this.pending) throw new Error("Answer receipt could not be verified");
      await this.synchronize(g);
    } catch (error) {
      if (!this.alive(g) || this.pending?.submissionId !== pending.submissionId)
        return;
      if (
        error instanceof ServiceError &&
        error.detail.code === "ALREADY_ANSWERED" &&
        error.detail.receipt
      ) {
        this.accept(error.detail.receipt);
        await this.synchronize(g);
        return;
      }
      this.submission = "unconfirmed";
      this.message =
        "Result not yet confirmed. Your selection is saved; retry will check the server before sending again.";
      if (
        error instanceof ServiceError &&
        ["UNAUTHENTICATED", "EPOCH_MISMATCH", "NOT_JOINED"].includes(
          error.detail.code,
        )
      )
        this.fail(error);
      else if (error instanceof ServiceError && !error.detail.retryable) {
        // Definitive rejections may release the intent, but must not award points.
        this.clearPending();
        this.submission = "idle";
        this.message = error.message;
      }
    } finally {
      if (this.sending === pending) this.sending = null;
    }
  }

  private accept(receipt: Receipt) {
    if (
      !this.state ||
      receipt.quizId !== this.quizId ||
      receipt.epoch !== this.state.epoch ||
      receipt.questionId !== this.pending?.questionId
    )
      return;
    this.showFeedback(receipt);
    this.message = "";
    this.clearPending();
    // Deliberately do not assign totalScoreAtAcceptance to the current score.
  }

  private clearPending() {
    try {
      this.deps.storage.removeItem(this.key());
    } catch {
      // A retained intent will reconcile against the immutable receipt on reload.
    }
    this.pending = null;
  }

  private async resolvePending(g: number) {
    if (!this.pending || !this.state) return;
    const receipt = this.state.receipts.find(
      (r) => r.questionId === this.pending?.questionId,
    );
    if (receipt) {
      this.accept(receipt);
      return;
    }
    await this.sendPending(g);
  }

  async retry() {
    if (this.resolving || this.submission === "checking") return;
    const g = this.generation;
    this.resolving = this.synchronize(g);
    try {
      await this.resolving;
      if (
        this.alive(g) &&
        this.connection !== "expired" &&
        this.connection !== "reset" &&
        !this.ws
      )
        this.connect(g);
    } finally {
      this.resolving = null;
    }
  }

  next() {
    if (!this.feedback || !this.state || !this.canAdvance) return;
    this.dismissedVersion = this.feedback.acceptedVersion;
    try {
      this.deps.storage.setItem(
        `vocabulary.feedback.${this.quizId}`,
        JSON.stringify({
          epoch: this.state.epoch,
          participantId: this.state.participantId,
          version: this.dismissedVersion,
        }),
      );
    } catch {
      /* Dismissal is presentation only; no scoring action is sent. */
    }
    this.feedback = null;
    this.selected = "";
    this.submission = "idle";
    this.message = "";
    const latest = this.state.receipts.at(-1);
    if (latest && latest.acceptedVersion > this.dismissedVersion)
      this.showFeedback(latest);
    void this.startCurrentQuestion(this.generation);
  }

  private connect(g: number) {
    if (!this.alive(g)) return;
    this.closeSocket();
    const token = ++this.socketSequence;
    this.connection = "connecting";
    const ws = this.deps.socket(this.quizId);
    this.ws = ws;
    const valid = () => this.alive(g) && token === this.socketSequence;
    ws.onopen = () => {
      if (valid()) this.connection = "synchronizing";
    };
    ws.onmessage = (event) => {
      if (!valid()) return;
      try {
        const message = parseEvent(JSON.parse(event.data));
        if (message.type === "snapshot") this.receive(message.snapshot);
        else if (message.code === "SESSION_EXPIRED") {
          this.connection = "expired";
          this.message =
            "Your session has expired. Start a new session to play again.";
          this.closeSocket();
        } else if (message.code === "RESET_REQUIRED") {
          this.connection = "reset";
          this.message =
            "This quiz was reset. Rejoin to start in the new session.";
          this.closeSocket();
        } else {
          this.connection = "stale";
          this.standingsInterrupted = true;
          this.networkMessage =
            "Standings are temporarily unavailable. Showing last known scores.";
        }
      } catch {
        this.connection = "stale";
        this.networkMessage = "An update could not be verified. Reconnecting…";
        this.scheduleReconnect(g);
      }
    };
    ws.onclose = () => {
      if (valid()) this.scheduleReconnect(g);
    };
    ws.onerror = () => {
      if (valid()) this.scheduleReconnect(g);
    };
  }

  private scheduleReconnect(g: number) {
    this.closeSocket();
    if (
      !this.alive(g) ||
      this.reconnectTimer ||
      this.connection === "expired" ||
      this.connection === "reset"
    )
      return;
    this.connection = "reconnecting";
    this.standingsInterrupted = true;
    const delay =
      Math.min(30000, 500 * 2 ** Math.min(this.attempts++, 6)) *
      (0.8 + this.deps.random() * 0.4);
    this.reconnectTimer = setTimeout(async () => {
      this.reconnectTimer = undefined;
      await this.synchronize(g);
      if (
        this.alive(g) &&
        this.connection !== "expired" &&
        this.connection !== "reset"
      )
        this.connect(g);
    }, delay);
  }

  private fail(error: unknown) {
    if (error instanceof StorageUnavailableError) {
      this.message = error.message;
      this.connection = "unavailable";
      return;
    }
    if (error instanceof ServiceError) {
      if (error.detail.code === "UNAUTHENTICATED") {
        this.message = error.message;
        this.connection = "expired";
        this.closeSocket();
        return;
      }
      if (
        error.detail.code === "EPOCH_MISMATCH" ||
        error.detail.code === "NOT_JOINED"
      ) {
        this.message = error.message;
        this.connection = "reset";
        this.closeSocket();
        return;
      }
    }
    const message =
      error instanceof ServiceError
        ? error.message
        : "The service could not be reached. Please retry in a moment.";
    if (this.state) this.networkMessage = message;
    else this.message = message;
    this.connection = "unavailable";
  }

  async restartSession() {
    this.stop();
    this.busy = true;
    const g = this.generation;
    try {
      const identity = await this.deps.transport.session(true);
      if (!this.alive(g)) return;
      this.deps.storage.setItem("vocabulary.identity", identity);
      this.clearPending();
      await this.join(this.quizId, this.displayName);
    } catch (error) {
      if (this.alive(g)) this.fail(error);
    } finally {
      if (this.alive(g)) this.busy = false;
    }
  }

  leave() {
    this.stop();
    this.state = null;
    this.connection = "idle";
    try {
      this.deps.storage.removeItem("vocabulary.last");
    } catch {
      // Leaving never depends on browser persistence.
    }
    this.message = "";
    this.busy = false;
  }
  private closeSocket() {
    this.socketSequence++;
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.close();
      this.ws = null;
    }
  }
  stop() {
    this.generation++;
    this.closeSocket();
    clearTimeout(this.reconnectTimer);
    clearInterval(this.freshnessTimer);
    this.reconnectTimer = undefined;
    this.freshnessTimer = undefined;
    this.requestSequence = 0;
    this.appliedSequence = 0;
    this.resolving = null;
    this.sending = null;
    this.publicSync = null;
    this.starting = null;
    this.standingsInterrupted = false;
    this.attempts = 0;
  }
}
