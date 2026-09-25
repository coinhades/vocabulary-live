# AI usage

This document records where generative AI tools played a meaningful role in building Vocabulary Live, how I prompted them, and, most importantly, how I verified, tested, debugged and refined what they produced. Plain auto-completion is not listed. Every AI-assisted change went through the same gate as hand-written code: `make verify` (formatting, `go vet` and ESLint, `vue-tsc`, Go unit tests with the race detector, Vitest, Redis-backed integration tests, the production build, and both Playwright suites), plus a line-by-line read of the diff before it was kept.

- [Tools](#tools)
- [How I split work between the two coding assistants](#how-i-split-work-between-the-two-coding-assistants)
- [Working method for every AI-assisted change](#working-method-for-every-ai-assisted-change)
- [Boundaries: what was not AI-assisted](#boundaries-what-was-not-ai-assisted)
- [Summary of examples](#summary-of-examples)
- [Examples](#examples)
- [Prompting patterns that worked](#prompting-patterns-that-worked)
- [Lessons](#lessons)

## Tools

| Tool | Model | Used for |
| --- | --- | --- |
| Claude Code CLI | Claude Opus 5.5 | Reasoning-heavy peripheral code: clock and concurrency invariants, threat models, accessibility test oracles, image-processing math, documentation and adversarial review |
| Codex CLI | GPT-6 Astra | Self-contained scripts, table-driven tests from a precise spec, configuration and infrastructure files, regex and byte-level fixtures, tight run-observe-fix loops |
| GPT-image-2.1 | Image generation | The visual design: the dark-theme reference mockup and the decorative artwork set (book, trophy, correct/incorrect emblems, learner illustration, background, brand mark, avatars) |

The design of this application was generated with the GPT-image-2.1 model; see [Example 1](#1-visual-design-generated-with-gpt-image-21). Text, inputs, options, scores and participant names are never baked into images; they are real HTML so they remain accessible and testable.

## How I split work between the two coding assistants

I used both assistants throughout the project and chose per task rather than per language. The split below is what I observed on this codebase, not a general claim about the models.

| Kind of task | Tool I reached for | Why, in this project |
| --- | --- | --- |
| Deriving an invariant and the tests that would break it (three-clock countdown, credential-check coalescing, X-Forwarded-For trust boundary, audio playback races) | Claude (Opus 5.5) | It was more reliable at holding several files in context at once, explaining *why* before writing code, and volunteering edge cases I had not asked about. When I asked "how would you bypass this?" it produced concrete attack inputs rather than generic advice. |
| Test oracles for accessibility and UX behaviour (focus management, one-frame regressions, reduced motion) | Claude (Opus 5.5) | It reasoned well about ARIA semantics and about observing a *process* (insertions, animation events) rather than an end state. |
| Image math and algorithm explanation (un-blending from black, connected-component cutout) | Claude (Opus 5.5) | Multi-step numeric reasoning with trade-offs that I needed explained so I could tune thresholds myself. |
| Documentation prose (README system design, this file) | Claude (Opus 5.5) | Stronger at long, plain, non-promotional prose that keeps claims aligned with the code. |
| Table-driven Go and Vitest tests from a contract I wrote | Codex (GPT-6 Astra) | Faster and terser. It ran the tests itself in its sandbox, read the failure output, and patched until green, which suited mechanical coverage tables. |
| Self-contained scripts (`scripts/*.mjs`), Docker, Compose, GitHub Actions, ESLint and Playwright configuration | Codex (GPT-6 Astra) | Good recall of shell, Docker and CI idioms and of Node/Go standard-library APIs; good at small, complete files that I can review in one screen. |
| Byte-level or regex-level snippets (MP3 frame header, Vite asset fingerprint pattern, Unicode word boundaries) | Codex (GPT-6 Astra) | Quick to produce a candidate and a check for it; the check is what I kept. |
| Pairing | Both | For security-sensitive helpers I had one model draft and the other review the final diff adversarially, then I decided. |

Neither assistant had commit rights. Both ran inside the repository checkout with `OPENAI_API_KEY` unset; the real key lived only in my untracked `.env` and was never pasted into a prompt.

## Working method for every AI-assisted change

1. **Contract first.** I wrote the function signature, the invariants, and the "must never" rules before asking for code or tests (for example: "never infer a word from the sentence", "never grant time the server did not grant", "no automatic retry inside the HTTP helper").
2. **Constraints in the prompt.** No new dependencies, keep the existing signature, must pass `gofmt`/`go vet`/ESLint/`vue-tsc`, small diff, do not touch listed files.
3. **Tests before or alongside code.** For helpers I asked for the test table first and pruned it, then asked for the implementation to satisfy it.
4. **Adversarial pass.** For anything touching input parsing, headers, caching or storage I asked "how would you bypass or break this?" and turned the useful answers into test cases.
5. **Run the real gate.** `make verify` locally (Windows PowerShell and WSL) and in CI (`.github/workflows/verify.yml`, Ubuntu 24.04 with a real Redis service).
6. **Read every line.** Anything I could not explain was rewritten by hand or deleted. Several suggestions were rejected outright; they are noted below where instructive.

## Boundaries: what was not AI-assisted

The parts that decide whether scores are correct under concurrency, failure and reload were designed and written by hand, and the assistants' generated code was not accepted into them:

- `backend/internal/store/transition.lua`, `store.go`, `published.go`: the Redis Lua transitions (epoch, membership, idempotent receipts, one answer per question, deadline-based points, version increments) and snapshot reads.
- `backend/internal/domain/*`: quiz model, shuffled attempt plans, answer checking, submission validation, error codes.
- `backend/internal/realtime/hub.go`: the WebSocket hub, Pub/Sub subscription, refresh cycles, slow-client replacement.
- `backend/internal/httpapi/server.go`, `learning.go`, `instructor.go`, `review.go`: session, origin and membership checks and the command handlers.
- `backend/internal/learning/service.go`, `authoring.go`, `lessons.go`, `review.go`, `reflection.go`, `repository.go`, `gateway.go`, `provider.go`: the learning service, generation limits and caches, draft-to-publication workflow.
- `frontend/src/lib/quiz-machine.ts` and `quiz-machine.test.ts`, `composables/useQuiz.ts`, `useLearning.ts`, `App.vue`, `InstructorApp.vue`, the quiz components, and the runtime JSON validators in `frontend/src/types/protocol.ts`.
- The Redis integration suites (`*_integration_test.go`) and the browser journeys `journey.spec.ts`, `production.spec.ts`, `learning.spec.ts`, `instructor.spec.ts`, `review.spec.ts`.

For these files I used the assistants only as a reader: I asked them to list questions or risks after reading my code and treated the output as a checklist. Where a point was valid I wrote the fix and the regression test myself. I also used them as documentation lookups (go-redis option semantics, gorilla/websocket deadlines, Vue `Transition` hooks), which is not code generation.

## Summary of examples

| # | Area | Main files | Tool | Key verification |
| --- | --- | --- | --- | --- |
| 1 | Visual design | `frontend/public/art/*`, `scripts/extract-design-assets.ps1`, `frontend/src/tokens.css` | GPT-image-2.1 | Contrast checks, both themes at 320–1440 px, image-decoded assertions, no text baked into images |
| 2 | Art pipeline | `scripts/prepare-art.mjs` | Claude | Threshold tuning against visible artefacts, decoded-image assertions in Playwright |
| 3 | Prompt word emphasis | `frontend/src/lib/question-prompt.ts`, `.test.ts` | Codex | `it.each` tables incl. Unicode and combining marks; 16-word browser audit |
| 4 | Countdown presentation | `frontend/src/lib/countdown.ts`, `.test.ts`, `components/QuestionTimer.vue` | Claude | Monotonic-clock tests; `page.clock` wall-clock tampering e2e |
| 5 | HTTP boundary | `frontend/src/lib/http.ts`, `.test.ts` | Codex | Infinite-stream cap, proxy HTML rejection, malformed JSON, TS 5.9 typed-array generics |
| 6 | Pronunciation playback races | `frontend/src/lib/pronunciation.test.ts` (+ one guard in `pronunciation.ts`) | Claude | Deferred-fetch interleavings; found and fixed a state overwrite |
| 7 | Theme bootstrap and icons | `frontend/public/theme.js`, `src/lib/theme.ts`, `composables/useTheme.ts`, `components/AppIcon.vue` | Codex | Pre-paint theme e2e, explicit-choice persistence, no state change on toggle |
| 8 | Client IP behind proxies | `backend/internal/httpapi/client_ip.go`, `_test.go` | Claude + Codex | Adversarial header cases, per-client rate limits, `0.0.0.0/0` rejected |
| 9 | Rate limiter and static files | `backend/internal/httpapi/limiter.go`, `static.go`, `static_test.go` | Codex | Dotfile/traversal 404s, immutable caching, 304 handling, race detector |
| 10 | Configuration validation | `backend/internal/config/config.go`, `_test.go` | Claude + Codex | 22-case invalid table, no credential leakage, go-redis `MaxRetries` semantics |
| 11 | Credential probe coalescing | `backend/internal/learning/credentials.go`, `_test.go` | Claude | 12 goroutines to 1 probe, cancellation, in-flight override, no redirect follow |
| 12 | Deterministic test doubles | `backend/internal/testsupport/provider.go`, `cmd/learningtest`, `scripts/make-test-audio.mjs` | Codex | MP3 frame math, decoder reaches `playing`, no provider calls possible |
| 13 | Load benchmark driver | `backend/cmd/load/main.go` | Claude | UUID v4 fix after 400s, `accepted == offered`, `unobserved == 0` |
| 14 | Cross-platform task runner | `scripts/task.mjs`, `Makefile` | Codex | Windows `spawn` EINVAL fix, WSL and CI parity, child-process cleanup |
| 15 | Container and CI hardening | `Dockerfile`, `compose.yaml`, `compose.dev.yaml`, `.github/workflows/verify.yml` | Codex | In-binary healthcheck for `scratch`, CA bundle fix, read-only FS |
| 16 | Verification scripts | `scripts/compose-smoke.mjs`, `scripts/mutation-check.mjs` | Codex | Restart-retention assertions; mutant fails for the expected reason, original untouched |
| 17 | Accessibility and UX test oracles | `frontend/tests/learning/accessibility.spec.ts`, `tests/e2e/sync-notice.spec.ts`, `tests/e2e/motion.spec.ts` | Claude | 200% text overflow fix, MutationObserver instead of `toBeVisible`, animation sampling |
| 18 | Documentation | `README.md`, `AI_USAGE.md` | Claude | Every number cross-checked against code; mermaid rendered |

## Examples

### 1. Visual design generated with GPT-image-2.1

**Tool.** GPT-image-2.1.

**Task.** Produce the visual direction for the app: a full dark-theme reference screen (join card, live question, leaderboard) and a set of decorative assets: `book`, `trophy`, `correct`, `incorrect`, `learner`, the `Darkbg` backdrop, the `brand` mark and six small avatar portraits. The final files live in `frontend/public/art/`.

**Prompts and interaction.** I started with a layout prompt describing the product ("a multiplayer vocabulary quiz where learners answer at their own pace and see a live leaderboard"), a deep-navy palette and a flat illustration style with soft glow. After the first acceptable reference I sampled its colours and fixed them as CSS custom properties in `frontend/src/tokens.css` (`--paper #060a24`, `--forest #4decff`, `--lime #42f0ba`, `--violet #8849ff`, `--pink #f64e94`, `--surface #101837`). Every later artwork prompt reused those hex values so the emblems matched the UI. Emblem prompts asked for a single object on a solid black background so it could be un-blended later (Example 2), and I used negative constraints throughout: no rendered words, no UI chrome, no watermark, no drop shadows on the subject. The light theme's `learner` variant was a separate prompt because its hair, pupils and book text are opaque dark detail rather than glow.

**Verification and refinement.**

- Text is never part of an image. `scripts/extract-design-assets.ps1` only crops the brand mark and the six avatar circles from the reference (`Save-Crop` with an elliptical clip); headings, options, scores and names are HTML so screen readers, keyboard users and the 200% text test (Example 17) work.
- I checked the sampled text/background pairs against WCAG AA before committing them to `tokens.css` (`--muted #bdc5eb` on `--paper`, `--ink #f8f7ff` on `--surface`) and adjusted two generated colours that failed.
- Both themes were rendered at 320, 375, 768, 1024 and 1440 px by `tests/e2e/dark-theme.spec.ts` and `tests/e2e/light-theme.spec.ts`, which also assert that every `.word-art img` and `.join-illustration` has `complete && naturalWidth > 0` and that `document.documentElement.scrollWidth <= innerWidth`.
- Decorative art has no animation, so `prefers-reduced-motion` needs no special casing for it; the motion tests confirm zero running animations under reduced motion.
- Rejected outputs: several generated screens used gradient text and thin 1 px separators that did not survive the light theme; I kept the composition and replaced those details in CSS.

### 2. Art pipeline: un-blend and cut out artwork in headless Chromium

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** `scripts/prepare-art.mjs` converts the black-background WebP artwork into trimmed, transparent WebP files without adding an image library. It reuses the frontend's installed Playwright Chromium (`createImageBitmap`, `OffscreenCanvas`, `convertToBlob`) and supports two modes: `unblend` for luminous dark-theme emblems and `cutout` for the light-theme learner whose dark detail must stay opaque.

**Why Claude.** The task needed two pieces of image math explained well enough for me to tune them: recovering alpha from an image composited over black (`alpha = max(r, g, b)`, `colour = rgb * 255 / alpha`) and a connected-component flood fill that removes only border-connected near-black regions and "holes" of the same pure black, while keeping darker-but-not-pure shading such as pupils. I asked for the reasoning first and the code second.

**Prompts and interaction.**

> Constraint: no new npm dependency; use `@playwright/test`'s Chromium from `frontend/node_modules` via `createRequire`. Inputs are WebP with a black background in `docs/design/art`. Explain how to recover alpha for artwork composited over black, then explain how to separate real dark shading from background for an illustration with dark hair and pupils. Only after that, write one `convert` function that runs inside `page.evaluate`.

**Verification and refinement.**

- The first cutout pass punched holes through the learner's pupils. I added a per-region rule that a dark region is background only if it touches the border, or is at least 40 pixels with a mean brightness of 6.5 or lower; pupils are brighter on average and survive. The `floor = 8` noise threshold and `dark = 16` mask threshold were tuned by comparing output against the source at 1x and 2x on both theme backgrounds.
- A one-pixel soft band around the silhouette avoids a hard aliased edge; everything else is fully transparent or fully opaque, which keeps file sizes small.
- Emblems are capped near 2.5 times their largest CSS height (`maxHeight` 240 and 296) after I measured the rendered sizes; the book and learner keep source resolution.
- The script throws if `convertToBlob` does not return `image/webp` and logs `name width x height bytes` for each asset so regressions in size are visible.
- Playwright screenshot suites and the decoded-image assertions from Example 1 are the acceptance test; the `light-theme.spec.ts` join screens at five widths were where the learner cutout was judged.

### 3. Emphasising the vocabulary target in the prompt heading

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** `frontend/src/lib/question-prompt.ts` splits a question prompt into parts so the server-approved target word renders inside `<em>`. Contract: the caller supplies the word (`pronunciationText` from the server); the helper never infers a word from sentence structure or from an answer option; matches respect Unicode word boundaries; regex metacharacters in the word are literal; the parts must join back to the original text.

**Why Codex.** A compact regex helper plus `it.each` coverage tables is exactly where a fast run-fix loop pays off. Codex ran Vitest itself after each change.

**Prompts and interaction.**

> Write the Vitest tables first, in three groups: (a) the declared word is emphasised exactly once and `parts.map(p => p.text).join("")` equals the input; (b) no emphasis when the word is missing, empty, whitespace, absent from the text, or only a partial match (prefix, suffix, digits, underscore, diacritics, combining marks); (c) literal text and case are preserved for `Reserve/RESERVE/reserve`, `check-in`, `take off`, `café/CAFÉ`, `a.b`, `C++`, and a padded ` reserve `. Then implement the function so the tables pass. Do not add dependencies.

**Verification and refinement.**

- The first implementation used `\b`, which failed the `"écar caré car\u0301 car2 car_name"` and `"A CAFÉ beside the café."` cases because `\b` is ASCII-only. It was replaced by explicit lookarounds `(?<![\p{L}\p{M}\p{N}_])…(?![\p{L}\p{M}\p{N}_])` with the `giu` flags.
- I added the combining-mark case (`car\u0301`) after asking "which Unicode inputs would still break this?"; `\p{M}` in the lookarounds is the answer.
- `npx vitest run src/lib/question-prompt.test.ts`, `npm run typecheck` and `npm run lint` pass; the regex escaping of `[.*+?^${}()|[\]\\]` was checked with the `a.b` and `C++` rows.
- `frontend/tests/learning/question-highlight.spec.ts` audits all 16 seeded words in both themes: the heading's `em` text equals the declared word, restored questions after reload keep the same emphasis, and `vocab-q2` and `vocab-q8`, whose target is hidden among the options until acceptance, render no `em` at all. That test also asserts `question.pronunciationText` equals the expected word or is undefined, so the "never infer" rule is checked end to end.

### 4. Countdown presentation anchored to the server clock

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** `frontend/src/lib/countdown.ts` turns a server `QuestionClock` (`serverTimeMs`, `deadlineMs`), the page-monotonic time at which it was received, and the current `performance.now()` into `{ seconds, fraction }` for `QuestionTimer.vue`. It is presentation only; scoring uses Redis time on the server. The invariant is that editing the local wall clock or `sessionStorage` cannot extend the displayed deadline, and a background-tab gap catches up.

**Why Claude.** The helper juggles three clocks (Redis, browser wall clock, browser monotonic clock). I wanted the formula derived and each `Math.min`/`Math.max` justified, plus tests that would fail if someone swapped in `Date.now()`.

**Prompts and interaction.**

> Derive `remaining` from `deadlineMs - serverTimeMs` and the monotonic elapsed time `now - receivedAt`, clamped to `[0, durationSeconds * 1000]`. Explain why `receivedAt` must come from `performance.now()` and not `Date.now()`. Then propose Vitest cases that distinguish a correct implementation from one that trusts the wall clock or that restarts the countdown on reload.

**Verification and refinement.**

- `countdown.test.ts` covers: the bar moves continuously within one displayed second (`fraction` delta of `0.016 / 20`); a 15 s background gap yields `seconds: 5, fraction: 0.25`; a restored clock with 500 ms left shows `1` then `0` without granting new time; `null` clock shows the full bar; an already-expired clock shows `0`.
- `QuestionTimer.vue` (hand-written around the helper) uses `requestAnimationFrame`, switches to a 1 s `setInterval` under `prefers-reduced-motion`, and stops ticking while `document.hidden`; the `schedule` function re-anchors `now` on visibility change so the catch-up test scenario matches real behaviour.
- End to end, `tests/e2e/dark-theme.spec.ts` fixes the wall clock with `page.clock.setFixedTime`, advances it 7 s, reloads, and asserts the server `deadlineMs` from `POST /start` is unchanged and the timer still renders; after advancing a full duration there are still zero answer requests and zero receipts. Time spent reading feedback does not consume the next word's countdown (asserted by `"${next.timing.durationSeconds} seconds left"` after `Next question`).
- The suggestion to show negative seconds after expiry as "overtime" was rejected; the server's 50-point rule is explained in `timer-expiry` text instead.

### 5. HTTP boundary helper with a streaming size cap

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** `frontend/src/lib/http.ts`: `requestJSON` sends one same-origin request with `cache: "no-store"`, combines a timeout with an external `AbortSignal` via `AbortSignal.any`, rejects any response that is not `application/json` (proxy HTML error pages), and reads the body through `readResponse`, which enforces a byte cap and cancels the stream instead of buffering the rest. Structured failures are preserved as `ServiceError` carrying the parsed `APIError`. Retries deliberately do not live here; they belong to the quiz state machine.

**Why Codex.** Plumbing over well-known Web APIs with mock-heavy tests; the model produced the `ReadableStream` test double quickly and iterated against Vitest.

**Prompts and interaction.**

> Requirements: no retry logic; 6 s default timeout; 256 KiB default cap; when the cap is exceeded call `reader.cancel()` and throw "too large"; a non-JSON `Content-Type` must be rejected before parsing and the body cancelled; tests must stub `fetch` with `vi.stubGlobal` and prove `cancel` is called exactly once for an infinite stream.

**Verification and refinement.**

- `http.test.ts`: a `ReadableStream` whose `pull` enqueues 16 bytes forever proves the cap fires and `cancel` is called once; a 502 `text/html` body never reaches `parse`; `{` rejects as malformed JSON; a 1,048,577-byte audio body is rejected by `learningAudio`; the `AI_CREDENTIALS_INVALID` failure notifies `onLearningCredentialsRejected` listeners exactly once.
- `vue-tsc` flagged the first draft's `Uint8Array` return type against `TextDecoder.decode`; the fix uses the TypeScript 5.7+ generic `Uint8Array<ArrayBuffer>` (project is on 5.9.3).
- I checked `AbortSignal.any` support (Chromium 116+, Node 20.3+) against `engines.node >= 24.14.0` and the Playwright Chromium in use.
- The first draft also cleared `reader.releaseLock()` only on success; it is now in `finally` so a thrown cap error does not leave the stream locked.

### 6. Pronunciation playback races

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** `PronunciationController` in `frontend/src/lib/pronunciation.ts` owns one `HTMLAudioElement`, a bounded blob cache (32 clips or 16 MiB, 24 h expiry) and a sequence token so late responses after `stop()` are ignored. I wrote the controller; I used Claude to enumerate interleavings that could leave the state inconsistent and to build the Vitest harness in `pronunciation.test.ts` with injected `AudioDependencies`.

**Why Claude.** The failure modes are orderings of promise resolution and media events (autoplay rejection, `onerror` firing during `play()`, navigation during fetch). It reasoned about each interleaving explicitly before proposing a test.

**Prompts and interaction.**

> Here is the controller. List interleavings between `fetch` resolution, `stop()`, `audio.play()` rejection and `onerror` that could produce a wrong `status` or leak a blob URL. For each, write a Vitest case with a fake audio element and a deferred fetch (`mockImplementation(() => new Promise(...))`). Do not change the controller yet.

**Verification and refinement.**

- The scenario "decoder `onerror` fires while `play()` is still pending, then `play()` rejects" exposed a real bug: the `catch` branch set `ready-to-play`, hiding the error. The controller now checks `this.state.status === "error"` in both the success and rejection paths after `await audio.play()`; the test `does not replace a decoder failure with a late play outcome` locks it in and asserts `revokeURL` was called once.
- Other cases: `NotAllowedError` leaves the clip cached and shows "Ready — tap to play." without a second fetch; a second `play()` for the same key while loading is a no-op (`fetch` called once) and a response arriving after `stop()` never calls `audio.play()`; `clear()` detaches `onended`/`onerror` and revokes every URL.
- In the browser, `tests/learning/accessibility.spec.ts` activates the speaker with the keyboard and asserts `.pronunciation-control[data-state="playing"]` appears while focus stays on the button, using the silent MP3 fixture from Example 12 so Chromium really decodes audio.

### 7. Theme bootstrap, theme rule and the icon set

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** Three small, self-contained pieces: `frontend/public/theme.js` applies the saved or system theme before first paint (so a light-theme visitor never sees a dark frame) and updates `meta[name="theme-color"]`; `src/lib/theme.ts` holds the same rule as `resolveTheme(saved, prefersLight)` with a Vitest test; `composables/useTheme.ts` follows system changes only while no explicit choice exists and syncs across tabs via the `storage` event; `components/AppIcon.vue` is a 24 px stroke icon set on `currentColor` with `aria-hidden="true"`.

**Why Codex.** Boilerplate-sized files where speed matters more than deliberation; the SVG paths were generated and then visually checked.

**Verification and refinement.**

- `theme.test.ts`: an explicit choice wins over the system preference; `null`, `"sepia"` and `""` fall back to the system.
- `tests/e2e/light-theme.spec.ts` asserts `html[data-theme="light"]`, `meta[theme-color]` equal to `#f6f5f0` and `body` background `rgb(246, 245, 240)` on first load; `emulateMedia` flips the theme both ways; an explicit toggle persists in `localStorage["vocabulary.theme"]` across reload; switching themes leaves receipts, score and current question identical.
- `theme.js` intentionally duplicates the rule because a pre-paint script cannot import the module; both files carry a comment pointing at each other, and the e2e colour assertion catches drift.
- Icons were checked at 16–24 px in both themes; the first `trophy` and `bulb` paths had uneven stroke joins and were redrawn. ESLint's `vue` rules pass with the project's flat config.

### 8. Client IP derivation behind trusted proxies

**Tools.** Claude Code CLI (Opus 5.5) for the threat model and implementation review; Codex CLI (GPT-6 Astra) for the table-driven tests.

**Task.** `backend/internal/httpapi/client_ip.go` decides which address the per-IP rate limiter keys on. If the TCP peer is not a trusted proxy, use the peer. Otherwise walk `X-Forwarded-For` from right to left, stopping at the first hop that is not trusted, because untrusted hops cannot vouch for anything to their left. Reject malformed entries and IPv6 zones, cap the list at 32 entries, and `Unmap()` IPv4-mapped IPv6 so `::ffff:192.0.2.1` and `192.0.2.1` share a bucket.

**Prompts and interaction.**

> Threat model: the attacker fully controls the `X-Forwarded-For` value; proxies listed in `TrustedProxies` append the address they observed. Write `clientIP` using `net/netip`. Then, separately, list concrete header values that would let a client obtain a different rate-limit bucket than it deserves.

The adversarial list produced the `spoofed prefix` (`198.51.100.9, 192.0.2.1` behind a trusted proxy must resolve to `192.0.2.1`), `too many` and `mapped` cases. Codex was then given the table and asked to produce `TestClientIPTrustBoundary` and a second test tying the result to the limiter.

**Verification and refinement.**

- `client_ip_test.go` covers direct peers, one proxy, a proxy chain, spoofed prefix, missing header, garbage entry, 33 entries, IPv6 and IPv4-mapped peers.
- `TestProxyClientsReceiveIndependentRateLimits` shows two learners behind one proxy get separate buckets and a forged prefix cannot escape a limit.
- Config guard (Example 10): `TRUSTED_PROXY_CIDRS` rejects `0.0.0.0/0`, `::/0` and non-CIDR values, after asking "which misconfiguration would make this trust everyone?".
- `go test -race ./internal/httpapi -run 'TestClientIP|TestProxyClients'`, `go vet -tags integration ./...`. `.env.example` documents that the proxy must overwrite or append the observed client.

### 9. Token-bucket limiter and the static file handler

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** `limiter.go`: a per-key token bucket with a bounded map (4096 keys), a lazy sweep of entries idle for more than two minutes at most once per minute, and denial of new keys while the map is full. `static.go`: serve `index.html` only for `/` and `/instructor`; reject backslashes, NUL and any dot-prefixed path segment; return 404 rather than the SPA for missing assets; mark Vite fingerprinted assets (`/assets/name-hash.ext`, hash of 8+ URL-safe characters) as `public, max-age=31536000, immutable`; everything else `no-cache`.

**Why Codex.** Standard idioms, small files, `httptest`-driven tests it could run itself.

**Verification and refinement.**

- `static_test.go` writes a temp directory containing `index.html`, a fingerprinted asset, `theme.js` and a `.env` file, then asserts status and `Cache-Control` for ten paths, including `/.env`, `/assets/../.env`, `/assets/` and `/fonts/missing.woff2`; the body must never contain `PRIVATE` and a 404 must never contain the SPA HTML. A conditional request with `If-Modified-Since` returns 304 while retaining the `immutable` header.
- I ran `npm run build` and inspected `frontend/dist/assets` to confirm Vite's hash length before settling on `{8,}`; `scripts/compose-smoke.mjs` fetches every `/assets/` URL referenced by the served `index.html` and expects 200.
- The limiter uses the Go 1.21+ `min` builtin; `go test -race` passes. The "deny new keys while full" behaviour was a deliberate choice over unbounded growth and is what `TestProxyClientsReceiveIndependentRateLimits` exercises with `newLimiter(1, 1)`.
- A suggested `sync.Map` variant was rejected: the read-modify-write on a bucket needs one mutex anyway.

### 10. Configuration validation

**Tools.** Claude Code CLI (Opus 5.5) produced the validation checklist; Codex CLI (GPT-6 Astra) produced the table test.

**Task.** `backend/internal/config/config.go` reads the environment through an injected `getenv`, validates every setting, and configures the go-redis client with explicit deadlines (700 ms dial/read/write, 800 ms pool, context timeouts on, no implicit retries, pool size 32).

**Prompts and interaction.** For `ALLOWED_ORIGINS` I asked Claude to enumerate what an "exact browser origin" excludes. The resulting rules are in code: scheme must be `http` or `https`; host required; optional port must parse in 1–65535 and a trailing `:` is an error; no path, query, fragment, userinfo, opaque part, wildcard, backslash or `#`; the literal `null` is rejected; duplicates are collapsed; production requires `https`. Codex then generated `TestInvalidConfiguration` from that list plus `HTTP_ADDR`, `REDIS_NAMESPACE`, `REDIS_URL`, rate-limit and proxy cases.

**Verification and refinement.**

- The table has 22 invalid inputs; each error must mention the offending key and must not contain `password` or `secret`. The `REDIS_URL` case uses `https://secret:password@redis.invalid` precisely to check that the error text does not echo credentials.
- A go-redis detail was caught by a test, not by the model: the initial draft set `MaxRetries: 0`, but go-redis treats `0` as "use the default of 3 retries" and `-1` as "disable". Reading `options.go` confirmed this; the code now sets `-1`, and `TestDefaultsAndRedisDeadlines` asserts the initialised client reports `MaxRetries == 0` and `ContextTimeoutEnabled == true`.
- `HealthURL()` maps a `0.0.0.0` or `::` listen address to loopback so the container healthcheck (Example 15) works; `TestProductionAndCustomHealthAddress` covers three addresses and confirms production rejects the default HTTP origins.

### 11. OpenAI credential probe with coalescing and override

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** `backend/internal/learning/credentials.go`: before exposing learning controls the server checks the key with a bounded `GET /v1/models` (2 s timeout, no body, 512 KiB response cap, no redirect following, provider error bodies discarded). Concurrent callers share one in-flight probe; results are cached for 5 min on success, 15 s on unavailability, 1 min on an explicit rejection; a real request that receives 401/403 calls `rejectCredentials()`, which bumps a version so an in-flight probe's stale success cannot overwrite the rejection; waiters can cancel via context.

**Why Claude.** The design question was why `sync.Once` or a plain singleflight is insufficient (results must expire and be overridable) and how to make the waiting path cancellable without leaking goroutines. It walked through the lock/unlock sequence and the version check before writing code.

**Verification and refinement.**

- `credentials_test.go`, run with `-race`: seven status/body combinations map to `AI_DISABLED`, success, `AI_CREDENTIALS_INVALID` or `AI_UNAVAILABLE`, and three sequential calls perform exactly one probe (zero when the key is empty); 12 concurrent callers plus one already-cancelled waiter produce one probe and the waiter returns `context.Canceled`; a POST that sees 401 while a probe is in flight wins, is cached, and recovers after expiry; a 307 redirect target is never contacted; a server that never responds fails within the 2 s bound (asserted under 3 s).
- The probe handler in the tests asserts method, path, `Authorization` header and an empty body, so the check cannot silently become a billable request.
- `response.go` maps provider failures to fixed user-facing messages, so discarded provider bodies never reach the browser. The opt-in `cmd/ai-smoke --allow-paid-api` tool was run once by hand against a real key to confirm wiring; it is excluded from the image and from `make verify`.

### 12. Deterministic test doubles and the audio fixture

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** Three pieces that let the learning UI be browser-tested without any provider call: `backend/internal/testsupport/provider.go`, a fake `learning` provider covering every action (`authoring`, `regenerate`, `explanation`, `example`, `reflection`, `review`, `hint`) with switchable failure modes and an artificial delay; `backend/cmd/learningtest`, a dedicated server on `127.0.0.1:18082` that cleans only the `vocab-ai-e2e:*` namespace with `SCAN`, and exposes `POST/GET /__test/provider` to switch modes; and `scripts/make-test-audio.mjs`, which writes a two-second silent MP3 from 80 zero-data frames.

**Why Codex.** Boilerplate-heavy fixtures with many near-identical branches, plus a byte-level header where a quick candidate and a check for it were what I needed.

**Verification and refinement.**

- MP3 header `FF FB 90 00` decodes as MPEG-1 Layer III, no CRC, 128 kbps, 44.1 kHz, no padding, stereo. Frame length is `144 * 128000 / 44100 = 417` bytes and each frame is 1152 samples (26.12 ms), so 80 frames are about 2.09 s. I verified the arithmetic by hand, confirmed `learning.ValidMP3` accepts the file, and relied on Chromium actually reaching `data-state="playing"` in `accessibility.spec.ts` as the decoder check. The fixture is silence, so no vocabulary speech ships with tests.
- The control endpoint rejects malformed JSON and delays outside 0–30 s with 400, added after asking for input bounds; the request body is limited to 1 KiB.
- `scripts/task.mjs serve-learning-e2e` starts the test server with `OPENAI_API_KEY` forced empty, and the `Dockerfile` builds only `./cmd/server`, so the test executable and fake provider cannot reach production.
- The learning Playwright suite (`learning.spec.ts`, `instructor.spec.ts`, `review.spec.ts`, `accessibility.spec.ts`, `question-highlight.spec.ts`) runs against this server in CI; the `long` mode was added to test text wrapping at 200% zoom.

### 13. Load benchmark driver

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** `backend/cmd/load/main.go` starts a real application on `httptest`, joins up to 400 native HTTP+WebSocket participants across one or two rooms, submits eight answers each under a concurrency semaphore and optional rate ticker, and measures answer response time and answer-to-other-socket propagation (p50/p95/p99). It writes a JSON report with Go, OS, CPU and Redis versions and fails if any accepted answer was never observed by another socket or if the driver dropped observations.

**Why Claude.** Measurement correctness has several traps: observing your own snapshot, sleeping a fixed time instead of waiting for the final room version, and silently dropping events under load. I wanted each addressed explicitly and explained in comments.

**Verification and refinement.**

- First run: every answer was rejected with `400 VALIDATION_ERROR`. The server validates `submissionId` as a UUID v4 (`domain.ValidSubmission`), while the driver used the raw 32-hex `domain.ID()`. The fix formats `8-4-4-4-12` with the version nibble `4` and variant `8`; the client-side `crypto.randomUUID()` already satisfies this, which is why the browser never saw the problem.
- Observations flow through a bounded channel; a `default:` branch counts drops and the run is declared invalid if any occurred, rather than reporting optimistic propagation numbers.
- The driver waits for each room's actual final `version` from a consistent snapshot (8 s deadline, 10 ms poll) instead of sleeping.
- Safety: the namespace must start with `vocab-bench`, the limiter is set explicitly to 10000/20000 for the benchmark and documented, and `make load-test` defaults to 100 participants over two rooms (800 answers) with `accepted == offered` and `unobserved == 0` as pass criteria. The README states the 200-participant room limit is an enforced cap, not a measured throughput claim.

### 14. Cross-platform task runner

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** `scripts/task.mjs` is the single entry point behind `Makefile` targets and CI: `build`, `dev` (Go and Vite together, either exit kills the other), `fmt-check` (gofmt over discovered `.go` files plus Prettier), `lint`, `typecheck`, `test-unit`, `test-integration`, `test-e2e`, `serve-e2e` (resets the `vocab-e2e` namespace, then serves with e2e-specific limits), `serve-learning-e2e`, `verify`, `demo-reset` and `load-test`.

**Why Codex.** A self-contained Node script where recall of `child_process` behaviour across platforms and exit-code propagation mattered.

**Verification and refinement.**

- On Windows, `spawnSync("npm", …)` failed with `EINVAL`. Node's April 2024 hardening (CVE-2024-27980) refuses to spawn `.cmd`/`.bat` without `shell: true`. Instead of enabling a shell and taking on quoting risks, the script resolves `npm-cli.js` next to `process.execPath` and runs it with Node directly (`invocation()`); the same code path works on Linux because the branch is guarded by `process.platform === "win32"`.
- Exit codes propagate (`process.exit(result.status ?? 1)`), `dev` registers `SIGINT`/`SIGTERM` handlers that kill both children, and `test-e2e` builds first if `dist/server` is missing so browser tests never run stale code.
- I ran `node scripts/task.mjs verify` in PowerShell and in WSL, and CI runs `make verify` on `ubuntu-24.04`. `.exe` suffix handling is covered by `scripts/local-verification.sh`, which cross-compiles `dist/server.exe` from WSL.
- A suggestion to run the two Playwright suites in parallel was rejected: they use different ports and namespaces but share the machine's CPU budget, and the timing assertions are more reliable sequentially.

### 15. Container and CI hardening

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** `Dockerfile`: Node and Go build stages, then a `scratch` runtime with only the binary, the built frontend and the CA bundle, running as `10001:10001`. `compose.yaml`: app bound to `127.0.0.1:${APP_PORT}`, `read_only`, `cap_drop: [ALL]`, `no-new-privileges`, healthchecks for app and Redis, Redis with AOF `everysec`, `maxmemory 256mb` and `noeviction`, a persistent volume, and stop grace periods. `compose.dev.yaml` exposes Redis on loopback only. `.github/workflows/verify.yml`: Redis service with a health command, pinned action versions, Go and npm caches, Playwright Chromium install, `make verify`, `docker compose build`, and evidence upload with `if: always()`.

**Verification and refinement.**

- The first healthcheck used `curl -f http://localhost:8080/readyz`, which cannot work in `scratch`. The app now has a `--healthcheck` flag that performs the GET against `config.HealthURL()` and exits non-zero on anything but 200; Compose calls `["CMD", "/app", "--healthcheck"]`.
- The first image failed the OpenAI credential probe with `x509: certificate signed by unknown authority` because `scratch` has no roots; `COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/` fixed it, verified by re-running the probe with a key on my machine.
- `read_only: true` was kept after confirming the process writes nothing to disk (logs go to stdout, no temp files); `docker compose ps` shows both services healthy after `docker compose up --build`.
- `scripts/compose-smoke.mjs` (Example 16) is the behavioural check for the container: readiness, assets, cookie flags, a restart of both services, and retained state.
- `noeviction` was chosen deliberately so Redis reports write errors instead of silently discarding quiz state under memory pressure; the README's trade-offs section notes the absence of replication and failover.

### 16. Verification scripts: Compose smoke and mutation experiment

**Tool.** Codex CLI (GPT-6 Astra).

**Task.** Two scripts whose only purpose is to check the system.

`scripts/compose-smoke.mjs` runs against a live Compose stack: `/readyz`; the SPA HTML references `/assets/` and every referenced asset returns 200; every `/api` response carries `Cache-Control: no-store`; the session cookie is `HttpOnly`; join, start and answer succeed; then it restarts `redis` and `app`, waits for readiness with a 30 s deadline, and asserts the private state is byte-for-byte identical, replaying the same submission returns `outcome: "replayed"` with the identical receipt, `start` returns the same `deadlineMs`, `/metrics` exposes the expected series and does not contain the cookie value.

`scripts/mutation-check.mjs` copies `backend/` into `.cache/mutation-<timestamp>/`, replaces exactly one guard in the *copy* of `transition.lua` (`if previous then return {'ALREADY_ANSWERED',previous} end`) with a comment, runs only `TestAcceptanceReplayAndUniqueness` with `-race -tags integration`, requires it to fail with the specific message `want ALREADY_ANSWERED got <nil>`, then verifies the original file's SHA-256 is unchanged and reruns the original test, which must pass.

**Why Codex.** Assertion-heavy scripting with process control; the model ran the scripts against a local stack and adjusted the readiness polling loop itself.

**Verification and refinement.**

- The mutation script asserts the guard occurs exactly once before mutating and throws if the failure reason differs, so a test that fails for an unrelated reason (for example a missing Redis) cannot be mistaken for detection. The Lua script and its regression test remain hand-written; the script only demonstrates that the test is sensitive to the guard it claims to protect.
- The smoke script keeps the session cookie in memory and never writes it; its evidence file records only PASS text. It asserts `no-store` only for `/api` paths because static assets intentionally carry other cache headers (Example 9).
- Both scripts were run in WSL against `docker compose -p elsa-verify`; the restart branch initially raced the app's start period, which is why readiness is polled every 100 ms up to 30 s instead of a fixed sleep.

### 17. Accessibility and UX test oracles

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** Browser tests that observe behaviour rather than a final screenshot:

- `tests/learning/accessibility.spec.ts`: a keyboard-only journey in both themes under reduced motion; native `<dialog>` focus must wrap (`Shift+Tab` from the close button lands on the last control); resolving asynchronous content must not move focus; every element's font size is doubled at 1440 px and 375 px and the page must not scroll horizontally; the speaker button stays vertically centred on the word title; the dialog animation is `none` under reduced motion; `Escape` returns focus to the opener; `Next question` moves focus to the question heading.
- `tests/e2e/sync-notice.spec.ts`: `page.routeWebSocket` withholds server snapshots; a `MutationObserver` installed with `addInitScript` counts `.network-notice` insertions, so a one-frame warning during normal navigation is a failure; a genuine socket close (`1012`) must insert exactly one notice that stays connected through reconnection and `Retry synchronization`, then disappears when the held snapshot is released, with receipts unchanged.
- `tests/e2e/motion.spec.ts`: records `animationstart` and `transitionrun` events, samples the real CSS keyframes at 0/120/360 ms with `getAnimations()`, and asserts that a reconnect reconciling the same receipt does not replay reward motion, that repeated clicks on `Next question` advance presentation once, and that under reduced motion zero animations are running at the end.

**Why Claude.** Designing oracles that detect transient states and focus regressions requires reasoning about what the DOM does over time, and about which ARIA behaviours are guaranteed by the browser versus by the app.

**Verification and refinement.**

- The 200% text pass found real overflow at 375 px on the word title line; the CSS was fixed and the test keeps the bounding-box assertion (`speakerBox.x > wordBox.x + wordBox.width`).
- The first version of the sync-notice test used `expect(notice).toHaveCount(0)` after navigation, which cannot see a notice that appears and disappears within a frame. The `MutationObserver` evidence object replaced it, and two `requestAnimationFrame` ticks are awaited while the first snapshot is withheld so painted frames are actually observed.
- Every spec collects `pageerror` events and asserts the list is empty; screenshots are written under `docs/evidence/screenshots/` and uploaded by CI as artifacts.
- These suites check presentation and recovery UX. Server-side correctness under concurrency is covered separately by the hand-written Redis integration tests.

### 18. Documentation

**Tool.** Claude Code CLI (Opus 5.5).

**Task.** The README's system design section (mermaid architecture diagram, component table, join-to-leaderboard data flow, stored-keys table, failure-handling table, technology choices, deployment trade-offs) and the structure and first draft of this file.

**Prompts and interaction.** I gave the model the package list and asked for plain descriptions without marketing language, then for each sentence that contained a number or a guarantee I asked it to point to the line of code that supports it.

**Verification and refinement.**

- Every numeric claim was checked against the source: 100/50/0 points and 20/30/40 s durations (`fixtures/study.go`), the 200-participant cap (`store.New(..., 200)`), 700 ms Redis timeouts and 32-connection pool (`config.go`), 45 s WebSocket read deadline with 15 s pings and 3 s write deadline (`hub.go`), 800 ms snapshot context, seven-day session expiry, 24 h review expiry, quotas of 20 drafts and 20 publications (`response.go` messages).
- Claims the code does not support were removed or qualified: the room limit is described as an enforced cap rather than a throughput figure, Redis is described as a single primary without failover, and per-process rate limits are called out as a scaling caveat.
- The mermaid diagram was rendered in GitHub's preview to confirm syntax, and every relative link in the README was clicked.
- For this file, I asked the model to draft each example from my notes and the files, then I corrected any description that did not match the code exactly and removed anything that overstated what a test proves.

## Prompting patterns that worked

- **Contract before code.** State the signature, the invariants and the "must never" list; ask for tests that would fail if an invariant were dropped. Examples 3, 4, 5.
- **Explain, then implement.** For anything with maths or concurrency, ask for the reasoning first and reject the code if the reasoning is wrong. Examples 2, 11, 13.
- **Adversarial second pass.** "How would you bypass this?" produced the useful cases in Examples 8, 9 and 10; "which misconfiguration would make this trust everyone?" led to rejecting `/0` prefixes.
- **One-line rationale per test row.** Asking for a short justification of each table row made redundant rows obvious and kept the tables small (Examples 3, 10).
- **Bounded scope.** "Do not touch these files", "no new dependencies", "return only a diff" kept changes reviewable and kept the assistants out of the core.
- **Paste the failure verbatim.** When debugging (400 from the load driver, `x509` in the container, `EINVAL` on Windows), giving the exact error and the relevant lines produced a correct explanation on the first try; open-ended "it does not work" prompts did not.
- **Model pairing.** One model drafts, the other reviews the final diff with the same threat model; disagreements were the most valuable signal.
- **Design prompts with fixed tokens.** Reusing the exact hex values from `tokens.css` in every GPT-image-2.1 prompt, plus negative constraints (no text, no UI chrome), made the artwork consistent enough to ship without manual recolouring.

## Lessons

- The assistants were most valuable where the surrounding code fixed the contract tightly: helpers, fixtures, scripts and tests. They were least reliable about library semantics that differ from the obvious reading (go-redis `MaxRetries`, Node's Windows `spawn` behaviour, `scratch` images having no shell or CA roots). Each of those was caught by a test or by running the real thing, never by reading the generated code alone.
- Generated tests are only as good as the contract in the prompt; the combining-mark and spoofed-prefix cases came from asking follow-up questions, not from the first answer.
- Keeping the core hand-written made review of AI output easier, because every generated file had a clear, narrow purpose and a clear owner for its correctness.
