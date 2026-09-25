<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import "./studio.css";
import AppIcon from "./components/AppIcon.vue";
import DraftQuestionEditor from "./components/DraftQuestionEditor.vue";
import { useTheme } from "./composables/useTheme";
import { learningRequest, learningError } from "./lib/learning-api";
import { ServiceError } from "./lib/api";
import { object, bool, text, list } from "./types/learning";
import {
  contexts,
  levels,
  parseDraft,
  parseOperator,
  parsePreview,
  parseSummary,
  type Draft,
  type DraftSummary,
  type Brief,
  type Preview,
} from "./types/authoring";
const { light, toggle: toggleTheme } = useTheme();
const authenticated = ref(false),
  checking = ref(true),
  disabled = ref(false),
  textConfigured = ref(false),
  secret = ref("");
const busy = ref(""),
  error = ref(""),
  notice = ref(""),
  conflict = ref<Draft | null>(null),
  drafts = ref<DraftSummary[]>([]);
const saved = ref<Draft | null>(null),
  edit = ref<Draft | null>(null),
  preview = ref<Preview | null>(null),
  previewLocalHash = ref("");
const finalReviewed = ref(false),
  targetWords = ref("");
const brief = ref<Brief>({
  topic: "",
  context: "daily-life",
  level: "B1",
  count: 4,
  targetWords: [],
});
const abort = new AbortController();
const intents = new Map<string, string>();
const reports = ref<
    { id: string; reason: string; createdAt: string; content: string }[]
  >([]),
  reportsLoaded = ref(false);
const dirty = computed(
  () =>
    !!edit.value &&
    !!saved.value &&
    JSON.stringify(payload(edit.value)) !==
      JSON.stringify(payload(saved.value)),
);
const canPublish = computed(
  () =>
    !!edit.value &&
    !dirty.value &&
    edit.value.status !== "published" &&
    edit.value.items.every((i) => !!i.approval) &&
    finalReviewed.value &&
    !busy.value,
);
function payload(d: Draft) {
  return {
    title: d.title,
    items: d.items.map((i) => ({ id: i.id, content: i.content })),
  };
}
function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
function itemDirty(id: string) {
  return (
    JSON.stringify(edit.value?.items.find((i) => i.id === id)?.content) !==
    JSON.stringify(saved.value?.items.find((i) => i.id === id)?.content)
  );
}
function actionID(action: string, body: unknown) {
  const key = JSON.stringify([action, body]);
  let id = intents.get(key);
  if (!id) {
    id = crypto.randomUUID();
    intents.set(key, id);
  }
  return id;
}
function request<T>(path: string, parse: (v: unknown) => T, body?: unknown) {
  return learningRequest(`/instructor${path}`, parse, body, abort.signal);
}
function apply(d: Draft) {
  saved.value = d;
  edit.value = clone(d);
  finalReviewed.value = false;
  preview.value = null;
  conflict.value = null;
  drafts.value = [
    parseSummary(d),
    ...drafts.value.filter((x) => x.id !== d.id),
  ];
}
async function loadList() {
  drafts.value = await request("/drafts", (v) => list(v, parseSummary, 20));
}
async function run(name: string, fn: () => Promise<void>) {
  if (busy.value) return;
  busy.value = name;
  error.value = "";
  notice.value = "";
  try {
    await fn();
  } catch (e) {
    if (abort.signal.aborted) return;
    if (
      e instanceof ServiceError &&
      e.detail.code === "OPERATOR_UNAUTHENTICATED"
    ) {
      authenticated.value = false;
      secret.value = "";
      error.value =
        "Your operator session expired. Sign in again; unsaved edits stay on this page.";
    } else if (
      e instanceof ServiceError &&
      e.detail.code === "VERSION_CONFLICT" &&
      saved.value
    ) {
      error.value =
        "A newer version was saved elsewhere. Your edits are still here. Compare the saved version before continuing.";
      try {
        conflict.value = await request(`/drafts/${saved.value.id}`, parseDraft);
      } catch {
        /* Keep local edits when recovery is unavailable. */
      }
    } else error.value = learningError(e);
  } finally {
    busy.value = "";
  }
}
async function login() {
  const credential = secret.value;
  secret.value = "";
  await run("Signing in", async () => {
    const s = await request("/session", parseOperator, { secret: credential });
    authenticated.value = true;
    textConfigured.value = s.textConfigured;
    await loadList();
  });
}
async function logout() {
  await run("Signing out", async () => {
    await request("/logout", (v) => bool(object(v).signedOut), {});
    authenticated.value = false;
    secret.value = "";
    saved.value = null;
    edit.value = null;
    drafts.value = [];
    preview.value = null;
    reports.value = [];
    reportsLoaded.value = false;
  });
}
async function create() {
  await run("Generating draft", async () => {
    const b = {
      ...brief.value,
      targetWords: targetWords.value
        .split(",")
        .map((w) => w.trim())
        .filter(Boolean),
    };
    if (b.targetWords.length && b.targetWords.length !== b.count) {
      error.value =
        "Provide one distinct target word per question, or leave the list empty.";
      return;
    }
    const body = { brief: b };
    apply(
      await request("/drafts", parseDraft, {
        ...body,
        clientActionId: actionID("create", body),
      }),
    );
    notice.value = "Draft saved. Every question needs instructor review.";
  });
}
async function open(id: string) {
  await run("Opening draft", async () => {
    apply(await request(`/drafts/${id}`, parseDraft));
  });
}
async function save() {
  if (!edit.value || !saved.value) return;
  const d = edit.value,
    body = { expectedVersion: saved.value.version, ...payload(d) };
  await run("Saving changes", async () => {
    apply(
      await request(`/drafts/${d.id}`, parseDraft, {
        ...body,
        clientActionId: actionID("save", body),
      }),
    );
    notice.value = "Saved. Changed questions need approval again.";
  });
}
async function approve(id: string) {
  if (!saved.value || dirty.value) return;
  const d = saved.value,
    body = { expectedVersion: d.version };
  await run("Saving approval", async () => {
    apply(
      await request(`/drafts/${d.id}/questions/${id}/approve`, parseDraft, {
        ...body,
        clientActionId: actionID(`approve-${id}`, body),
      }),
    );
    notice.value = "Question approved for this saved content.";
  });
}
async function regenerate(id: string) {
  if (!saved.value) return;
  const d = saved.value,
    body = { expectedVersion: d.version };
  previewLocalHash.value = JSON.stringify(
    edit.value?.items.find((i) => i.id === id)?.content,
  );
  await run("Preparing preview", async () => {
    preview.value = await request(
      `/drafts/${d.id}/questions/${id}/regenerate`,
      parsePreview,
      { ...body, clientActionId: actionID(`regenerate-${id}`, body) },
    );
    notice.value =
      "Preview ready. Your saved draft and local edits have not been replaced.";
  });
}
function replacePreview() {
  const p = preview.value,
    d = edit.value;
  if (!p || !d || !saved.value) return;
  const item = d.items.find((i) => i.id === p.questionId);
  if (
    !item ||
    p.draftId !== d.id ||
    p.baseVersion !== saved.value.version ||
    JSON.stringify(item.content) !== previewLocalHash.value
  ) {
    error.value =
      "This question changed while the preview was prepared. Keep your edits and request a fresh preview.";
    return;
  }
  item.content = clone(p.content);
  item.approval = null;
  preview.value = null;
  finalReviewed.value = false;
  notice.value =
    "Preview applied locally. Save and review this question again.";
}
async function publish() {
  if (!saved.value || !canPublish.value) return;
  const d = saved.value,
    body = { expectedVersion: d.version, finalReviewed: true };
  await run("Publishing new quiz", async () => {
    apply(
      await request(`/drafts/${d.id}/publish`, parseDraft, {
        ...body,
        clientActionId: actionID("publish", body),
      }),
    );
    notice.value = "New quiz published and ready to join.";
  });
}
function keepEditing() {
  if (!conflict.value || !saved.value || !edit.value) return;
  const remote = conflict.value;
  if (remote.status === "published") {
    error.value =
      "This saved draft has already been published. Open the saved version to view its immutable quiz.";
    return;
  }
  saved.value = remote;
  edit.value.version = remote.version;
  for (const i of edit.value.items) {
    i.approval = remote.items.find((x) => x.id === i.id)?.approval ?? null;
  }
  conflict.value = null;
  finalReviewed.value = false;
  notice.value =
    "Kept your local content against the latest saved version. Save explicitly to replace the saved fields.";
}
async function loadReports() {
  await run("Loading reports", async () => {
    reports.value = await request("/reports", (v) =>
      list(
        v,
        (row) => {
          const d = object(row),
            r = object(d.report);
          return {
            id: text(r.artifactId, 64),
            reason: text(r.reason, 40),
            createdAt: text(r.reportedAt, 64),
            content:
              d.artifact === null
                ? "Artifact expired; the report reference is retained."
                : JSON.stringify(object(d.artifact).data, null, 2).slice(
                    0,
                    6000,
                  ),
          };
        },
        20,
      ),
    );
    reportsLoaded.value = true;
  });
}
function beforeLeave(e: BeforeUnloadEvent) {
  if (dirty.value) {
    e.preventDefault();
    e.returnValue = "";
  }
}
onMounted(async () => {
  addEventListener("beforeunload", beforeLeave);
  try {
    const s = await request("/session", parseOperator);
    authenticated.value = true;
    textConfigured.value = s.textConfigured;
    await loadList();
  } catch (e) {
    if (e instanceof ServiceError && e.detail.code === "AUTHORING_DISABLED")
      disabled.value = true;
    else if (!(
      e instanceof ServiceError && e.detail.code === "OPERATOR_UNAUTHENTICATED"
    ))
      error.value = learningError(e);
  } finally {
    checking.value = false;
  }
});
onUnmounted(() => {
  abort.abort();
  removeEventListener("beforeunload", beforeLeave);
});
</script>
<template>
  <div class="app-shell studio">
    <header class="site-header">
      <a class="brand" href="/"
        ><span class="brand-mark" aria-hidden="true"
          ><img src="/art/brand.png" alt="" /></span
        ><span>Vocabulary <strong>Live</strong></span></a
      >
      <div class="header-actions">
        <button
          type="button"
          class="theme-toggle"
          :aria-label="light ? 'Switch to dark theme' : 'Switch to light theme'"
          @click="toggleTheme"
        >
          <AppIcon :name="light ? 'moon' : 'sun'" /></button
        ><button
          v-if="authenticated"
          type="button"
          class="learning-button"
          :disabled="!!busy || dirty"
          @click="logout"
        >
          Sign out
        </button>
      </div>
    </header>
    <main class="studio-main">
      <div class="studio-intro">
        <p class="eyebrow">INSTRUCTOR SPACE</p>
        <h1>Make room for new words.</h1>
        <p>
          Build a vocabulary practice, review every detail, then share a new
          quiz with your learners.
        </p>
      </div>
      <p v-if="checking" role="status">Checking operator session…</p>
      <section v-else-if="disabled" class="panel studio-card">
        <h2>Authoring is unavailable</h2>
        <p>This deployment has not enabled the instructor studio.</p>
        <a href="/" class="learning-button">Return to practice</a>
      </section>
      <form
        v-else-if="!authenticated"
        class="panel studio-card studio-login"
        @submit.prevent="login"
      >
        <h2>Operator sign in</h2>
        <p>This space is for the locally configured instructor.</p>
        <label
          >Operator secret<input
            v-model="secret"
            type="password"
            autocomplete="current-password"
            maxlength="512"
            required /></label
        ><button class="button primary" :disabled="!!busy">
          {{ busy || "Sign in" }}
        </button>
        <p class="learning-disclosure">
          The secret is exchanged for a one-hour session. It is never saved in
          browser storage.
        </p>
      </form>
      <div v-if="error" class="learning-error studio-message" role="alert">
        {{ error }}
      </div>
      <p v-if="notice" role="status" class="studio-message">{{ notice }}</p>
      <p v-if="busy" role="status" class="studio-message">{{ busy }}…</p>
      <template v-if="authenticated">
        <p v-if="!textConfigured" class="learning-error">
          AI generation is unavailable. Saved drafts can still be edited and
          reviewed.
        </p>
        <div class="studio-start">
          <form class="panel studio-card" @submit.prevent="create">
            <h2>A new practice</h2>
            <fieldset class="studio-fields" :disabled="!!busy || dirty">
              <legend class="sr-only">Draft brief</legend>
              <label class="studio-wide"
                >Topic<textarea
                  v-model="brief.topic"
                  maxlength="200"
                  rows="2"
                  placeholder="Everyday travel: planning a trip and asking for help"
                  required
                /></label
              ><label
                >Context<select v-model="brief.context">
                  <option v-for="v in contexts" :key="v">{{ v }}</option>
                </select></label
              ><label
                >Target level<select v-model="brief.level">
                  <option v-for="v in levels" :key="v">{{ v }}</option>
                </select></label
              ><label
                >Question count<select v-model.number="brief.count">
                  <option v-for="n in [4, 5, 6, 7, 8]" :key="n" :value="n">
                    {{ n }} questions
                  </option>
                </select></label
              ><label
                >Target words (optional)<input
                  v-model="targetWords"
                  maxlength="650"
                  placeholder="One per question, comma separated"
              /></label>
            </fieldset>
            <p class="learning-disclosure">
              Requested content is sent to an AI provider. Level, difficulty and
              tag proposals require your review.
            </p>
            <button
              class="button primary"
              :disabled="!!busy || dirty || !textConfigured"
            >
              Generate draft
            </button>
          </form>
          <section class="panel studio-card studio-drafts">
            <h2>Saved drafts</h2>
            <p class="learning-disclosure">
              Up to 20 drafts, retained for 30 days. Published quizzes remain
              available.
            </p>
            <p v-if="!drafts.length">Your first draft will appear here.</p>
            <ul v-else>
              <li v-for="d in drafts" :key="d.id">
                <button
                  type="button"
                  class="studio-draft-link"
                  :disabled="!!busy || dirty"
                  @click="open(d.id)"
                >
                  <strong>{{ d.title }}</strong
                  ><span
                    >{{
                      d.status === "published" ? "Published" : "Needs review"
                    }}
                    · Version {{ d.version }}</span
                  >
                </button>
              </li>
            </ul>
            <p v-if="dirty" class="learning-disclosure">
              Save or discard edits before opening another draft.
            </p>
          </section>
        </div>
        <section
          v-if="edit"
          class="studio-edit"
          aria-labelledby="draft-heading"
        >
          <div class="studio-edit-heading">
            <div>
              <p class="eyebrow">
                {{
                  edit.status === "published"
                    ? "PUBLISHED SNAPSHOT"
                    : "DRAFT · INSTRUCTOR REVIEW"
                }}
              </p>
              <h2 id="draft-heading">{{ edit.title }}</h2>
              <p>
                Version {{ saved?.version }} · {{ edit.items.length }} questions
                · {{ dirty ? "Unsaved changes" : "Saved" }}
              </p>
            </div>
            <div v-if="edit.status !== 'published'" class="learning-actions">
              <button
                type="button"
                class="learning-button"
                :disabled="!dirty || !!busy"
                @click="saved && apply(clone(saved))"
              >
                Discard local edits</button
              ><button
                type="button"
                class="button primary"
                :disabled="!dirty || !!busy || !!conflict"
                @click="save"
              >
                Save changes
              </button>
            </div>
          </div>
          <section
            v-if="conflict"
            class="panel studio-card studio-conflict"
            aria-label="Version conflict"
          >
            <h3>Compare with saved version {{ conflict.version }}</h3>
            <p>
              Your edits are preserved below. Replacing the saved fields
              requires another explicit save.
            </p>
            <details>
              <summary>Show saved content</summary>
              <pre>{{ JSON.stringify(payload(conflict), null, 2) }}</pre>
            </details>
            <div class="learning-actions">
              <button
                type="button"
                class="learning-button"
                @click="apply(clone(conflict!))"
              >
                Use saved version</button
              ><button
                type="button"
                class="learning-button"
                @click="keepEditing"
              >
                Keep my edits
              </button>
            </div>
          </section>
          <label class="studio-title"
            >Quiz title<input
              v-model="edit.title"
              maxlength="100"
              :disabled="edit.status === 'published' || !!busy"
          /></label>
          <p class="learning-disclosure">
            Review meaning, sense, answer key and every distractor. All options
            must be plausible and only one correct. Timers and 100/50 scoring
            follow the existing quiz rules.
          </p>
          <DraftQuestionEditor
            v-for="(item, index) in edit.items"
            :key="item.id"
            v-model="item.content"
            :number="index + 1"
            :approved="!!item.approval"
            :dirty="itemDirty(item.id)"
            :can-approve="!dirty"
            :busy="!!busy"
            :locked="edit.status === 'published'"
            :text-configured="textConfigured"
            @approve="approve(item.id)"
            @regenerate="regenerate(item.id)"
          />
          <section
            v-if="preview"
            class="panel studio-card studio-preview"
            aria-label="Regeneration preview"
          >
            <p class="eyebrow">AI PREVIEW · NOT SAVED</p>
            <h3>{{ preview.content.word }}</h3>
            <dl>
              <template v-for="(value, key) in preview.content" :key="key"
                ><dt>{{ key }}</dt>
                <dd>
                  {{ Array.isArray(value) ? value.join(" · ") : value }}
                </dd></template
              >
            </dl>
            <div class="learning-actions">
              <button
                type="button"
                class="learning-button"
                @click="preview = null"
              >
                Keep current question</button
              ><button
                type="button"
                class="button primary"
                @click="replacePreview"
              >
                Use new version
              </button>
            </div>
          </section>
          <section
            v-if="edit.status === 'published'"
            class="panel studio-card studio-published"
          >
            <h3>Your new quiz is ready</h3>
            <p class="studio-code">{{ edit.publishedQuizId }}</p>
            <a
              class="button primary"
              :href="`/?quiz=${encodeURIComponent(edit.publishedQuizId)}`"
              target="_blank"
              rel="noopener"
              >Open new quiz</a
            >
            <p class="learning-disclosure">
              The published questions are frozen. This code opens a separate
              quiz.
            </p>
          </section>
          <section v-else class="panel studio-card">
            <h3>Ready to share?</h3>
            <p>
              {{
                edit.items.filter((i) => i.approval && !itemDirty(i.id)).length
              }}
              of {{ edit.items.length }} questions approved.
            </p>
            <div class="studio-check">
              <input
                id="final-review"
                v-model="finalReviewed"
                type="checkbox"
                :disabled="dirty || !!busy"
              /><label for="final-review"
                >I have reviewed every question, its answer key and teaching
                content for this practice.</label
              >
            </div>
            <button
              type="button"
              class="button primary"
              :disabled="!canPublish"
              @click="publish"
            >
              Publish as new quiz
            </button>
            <p class="learning-disclosure">
              Creates one new quiz. Up to 20 published quizzes per deployment.
            </p>
          </section>
        </section>
        <section class="panel studio-card studio-reports">
          <h2>Learning reports</h2>
          <p>
            Review the 20 most recent learner reports. AI content can be
            confusing or incorrect.
          </p>
          <button
            type="button"
            class="learning-button"
            :disabled="!!busy"
            @click="loadReports"
          >
            Load recent reports
          </button>
          <p v-if="reportsLoaded && !reports.length">No reports to review.</p>
          <details v-for="r in reports" :key="r.id + r.createdAt">
            <summary>{{ r.reason }} · {{ r.createdAt }}</summary>
            <pre>{{ r.content }}</pre>
          </details>
        </section>
      </template>
    </main>
  </div>
</template>
