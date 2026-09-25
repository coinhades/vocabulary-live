// A two-second, silent MPEG-1 Layer III clip made of zero-data frames.
// Test decoder/lifecycle input only; never vocabulary speech or product audio.
import { mkdirSync, writeFileSync } from "node:fs";
const frame = Buffer.alloc(417);
frame.set([0xff, 0xfb, 0x90, 0x00]);
mkdirSync("frontend/tests/fixtures", { recursive: true });
writeFileSync(
  "frontend/tests/fixtures/test-tone.mp3",
  Buffer.concat(Array.from({ length: 80 }, () => frame)),
);
