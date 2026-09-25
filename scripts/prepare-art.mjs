#!/usr/bin/env node
// Converts the black-background artwork in docs/design/art into transparent,
// trimmed WebP files in frontend/public/art. Uses the frontend's installed
// Playwright Chromium for decoding and encoding; no image package is needed.
import { createRequire } from "node:module";
import { readFile, writeFile } from "node:fs/promises";

const require = createRequire(
  new URL("../frontend/package.json", import.meta.url),
);
const { chromium } = require("@playwright/test");
const source = new URL("../docs/design/art/", import.meta.url);
const target = new URL("../frontend/public/art/", import.meta.url);
// Emblems are capped near 2.5 times their largest CSS height; the book and
// learner keep their source resolution. Luminous dark-theme art is unblended
// from black. The light-theme learner has opaque dark detail (hair, text,
// pupils), so only its connected black background is cut out.
const assets = [
  { name: "book", maxHeight: Infinity, quality: 0.85, mode: "unblend" },
  { name: "correct", maxHeight: 240, quality: 0.9, mode: "unblend" },
  { name: "incorrect", maxHeight: 240, quality: 0.9, mode: "unblend" },
  { name: "trophy", maxHeight: 296, quality: 0.9, mode: "unblend" },
  { name: "learner", maxHeight: Infinity, quality: 0.86, mode: "cutout" },
];

async function convert({ base64, maxHeight, quality, mode }) {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  const bitmap = await createImageBitmap(
    new Blob([bytes], { type: "image/webp" }),
    { colorSpaceConversion: "none", premultiplyAlpha: "none" },
  );
  const { width, height } = bitmap;
  const full = new OffscreenCanvas(width, height).getContext("2d");
  full.drawImage(bitmap, 0, 0);
  const image = full.getImageData(0, 0, width, height);
  const pixels = image.data;
  // Lossy compression leaves faint noise in the black background.
  const floor = 8;
  const visible = 4;
  let left = width;
  let top = height;
  let right = -1;
  let bottom = -1;
  const background = mode === "cutout" ? cutoutMask() : null;
  function cutoutMask() {
    const count = width * height;
    const light = new Uint8Array(count);
    for (let p = 0; p < count; p++)
      light[p] = Math.max(pixels[p * 4], pixels[p * 4 + 1], pixels[p * 4 + 2]);
    const dark = 16;
    const mask = new Uint8Array(count);
    const label = new Int32Array(count).fill(-1);
    const neighbours = (p) => {
      const x = p % width;
      return [
        x > 0 ? p - 1 : -1,
        x < width - 1 ? p + 1 : -1,
        p - width,
        p + width,
      ].filter((q) => q >= 0 && q < count);
    };
    // Every dark region is either the border-connected background, a hole of
    // the same near-pure black (between hair strands and leaves), or real
    // shading such as pupils and book gaps, which is brighter on average.
    for (let seed = 0; seed < count; seed++) {
      if (label[seed] !== -1 || light[seed] > dark) continue;
      const region = [seed];
      label[seed] = seed;
      let edge = false;
      let sum = 0;
      for (let i = 0; i < region.length; i++) {
        const p = region[i];
        const x = p % width;
        const y = (p / width) | 0;
        sum += light[p];
        if (x === 0 || y === 0 || x === width - 1 || y === height - 1)
          edge = true;
        for (const q of neighbours(p))
          if (label[q] === -1 && light[q] <= dark) {
            label[q] = seed;
            region.push(q);
          }
      }
      if (edge || (region.length >= 40 && sum / region.length <= 6.5))
        for (const p of region) mask[p] = 1;
    }
    // Soften a narrow band around the silhouette; everything else is either
    // fully transparent background or fully opaque artwork.
    const band = new Uint8Array(count);
    for (let p = 0; p < count; p++)
      if (neighbours(p).some((q) => mask[q] !== mask[p])) band[p] = 1;
    const soft = band.slice();
    for (let p = 0; p < count; p++)
      if (!band[p] && neighbours(p).some((q) => band[q])) soft[p] = 1;
    return { light, mask, soft };
  }
  for (let i = 0; i < pixels.length; i += 4) {
    let rgb;
    let alpha;
    if (background) {
      const p = i / 4;
      rgb = [pixels[i], pixels[i + 1], pixels[i + 2]];
      alpha = background.soft[p]
        ? Math.min(
            255,
            (Math.max(0, background.light[p] - 10) * 255) / (72 - 10),
          )
        : background.mask[p]
          ? 0
          : 255;
    } else {
      rgb = [pixels[i], pixels[i + 1], pixels[i + 2]].map(
        (value) => (Math.max(0, value - floor) * 255) / (255 - floor),
      );
      alpha = Math.max(...rgb);
    }
    // Unblend from black: composited over black this reproduces the source.
    for (let channel = 0; channel < 3; channel++)
      pixels[i + channel] = alpha
        ? Math.min(255, (rgb[channel] * 255) / alpha)
        : 0;
    pixels[i + 3] = alpha;
    if (alpha > visible) {
      const x = (i / 4) % width;
      const y = Math.floor(i / 4 / width);
      left = Math.min(left, x);
      right = Math.max(right, x);
      top = Math.min(top, y);
      bottom = Math.max(bottom, y);
    }
  }
  full.putImageData(image, 0, 0);
  const pad = 6;
  const x = Math.max(0, left - pad);
  const y = Math.max(0, top - pad);
  const w = Math.min(width, right + 1 + pad) - x;
  const h = Math.min(height, bottom + 1 + pad) - y;
  const scale = Math.min(1, maxHeight / h);
  const cropped = new OffscreenCanvas(
    Math.round(w * scale),
    Math.round(h * scale),
  );
  const context = cropped.getContext("2d");
  context.imageSmoothingQuality = "high";
  context.drawImage(
    full.canvas,
    x,
    y,
    w,
    h,
    0,
    0,
    cropped.width,
    cropped.height,
  );
  const blob = await cropped.convertToBlob({ type: "image/webp", quality });
  if (blob.type !== "image/webp") throw new Error("WebP encoding unavailable");
  const encoded = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",")[1]);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
  return { encoded, width: cropped.width, height: cropped.height };
}

const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  for (const { name, maxHeight, quality, mode } of assets) {
    const input = await readFile(new URL(`${name}.webp`, source));
    const { encoded, width, height } = await page.evaluate(convert, {
      base64: input.toString("base64"),
      maxHeight,
      quality,
      mode,
    });
    const output = Buffer.from(encoded, "base64");
    await writeFile(new URL(`${name}.webp`, target), output);
    console.log(`${name}.webp ${width}x${height} ${output.length} bytes`);
  }
} finally {
  await browser.close();
}
