import { test, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
for (const theme of ["dark", "light"] as const) {
  test(`${theme}: keyboard, reduced motion and doubled text`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
        colorScheme: theme,
        reducedMotion: "reduce",
        viewport: { width: 1440, height: 900 },
      }),
      page = await context.newPage();
    await page.goto("/");
    await page
      .getByLabel("Your display name")
      .fill("A learner with a long name");
    await expect(
      page.getByRole("button", { name: "Join the quiz" }),
    ).toBeEnabled();
    await page.getByRole("button", { name: "Join the quiz" }).focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("radio").first()).toBeVisible();
    await page.getByRole("radio").first().focus();
    await page.keyboard.press("Space");
    await page
      .getByRole("button", { name: "Check answer", exact: true })
      .focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".answer-feedback")).toBeVisible();
    const speaker = page.locator(".word-title-line .speaker-button");
    await speaker.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.locator(
        '.pronunciation-control[data-state="playing"] .speaker-button',
      ),
    ).toBeVisible();
    await expect(speaker).toBeFocused();
    await page.getByRole("button", { name: "Examples", exact: true }).focus();
    await page.keyboard.press("Enter");
    const modal = page.getByRole("dialog");
    await expect(modal).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Close learning window" }),
    ).toBeFocused();
    // Native modal focus must wrap instead of reaching the live quiz behind it.
    await page.keyboard.press("Shift+Tab");
    await expect(
      page.getByRole("button", { name: "Create example" }),
    ).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(
      page.getByRole("button", { name: "Close learning window" }),
    ).toBeFocused();
    await page.getByLabel("Context", { exact: true }).selectOption("school");
    await page.request.post("/__test/provider", {
      data: { mode: "long", delayMS: 0 },
    });
    const example = page.getByRole("button", { name: "Create example" });
    await example.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".generated-text")).toContainText(
      "During our careful discussion",
    );
    // Resolving asynchronous content must not move the keyboard focus.
    await expect(
      page.getByRole("button", { name: "Another example" }),
    ).toBeFocused();
    await mkdir("../docs/evidence/screenshots/learning-ui-polish", {
      recursive: true,
    });
    for (const [width, height] of [
      [1440, 900],
      [375, 812],
    ]) {
      await page.setViewportSize({ width, height });
      await page.evaluate(() => {
        document
          .querySelectorAll<HTMLElement>("[data-base-font]")
          .forEach((el) => {
            el.style.removeProperty("font-size");
            delete el.dataset.baseFont;
          });
        const rows = Array.from(
          document.querySelectorAll<HTMLElement>(
            "body,main,section,div,h1,h2,h3,h4,p,span,button,a,label,input,select,option,dt,dd,li,summary,strong,small",
          ),
        ).map((el) => ({
          el,
          size: getComputedStyle(el).fontSize,
        }));
        for (const { el, size } of rows) {
          el.dataset.baseFont = size;
          el.style.setProperty(
            "font-size",
            `${parseFloat(size) * 2}px`,
            "important",
          );
        }
      });
      const overflow = await page.evaluate(() => ({
        width: innerWidth,
        scroll: document.documentElement.scrollWidth,
        elements: Array.from(document.querySelectorAll<HTMLElement>("body *"))
          .filter((el) => {
            const r = el.getBoundingClientRect();
            return r.width > 0 && r.right > innerWidth + 1;
          })
          .map((el) => el.tagName + "." + el.className)
          .slice(0, 16),
      }));
      expect(overflow.scroll, JSON.stringify(overflow)).toBeLessThanOrEqual(
        width,
      );
      const wordBox = await page.locator(".word-title-line h3").boundingBox();
      const speakerBox = await speaker.boundingBox();
      expect(speakerBox!.x).toBeGreaterThan(wordBox!.x + wordBox!.width);
      expect(speakerBox!.y + speakerBox!.height / 2).toBeCloseTo(
        wordBox!.y + wordBox!.height / 2,
        0,
      );
      expect(
        await modal.evaluate((el) => el.scrollWidth <= el.clientWidth),
      ).toBe(true);
      await page
        .getByRole("button", { name: "Another example" })
        .scrollIntoViewIfNeeded();
      await page.screenshot({
        path: `../docs/evidence/screenshots/learning-ui-polish/${theme}-text200-${width}.png`,
        fullPage: false,
        animations: "disabled",
      });
      expect(
        await page
          .locator(".learning-dialog[open]")
          .evaluate((el) => getComputedStyle(el).animationName),
      ).toBe("none");
    }
    await page.keyboard.press("Escape");
    await expect(modal).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Examples", exact: true }),
    ).toBeFocused();
    await page
      .getByRole("button", { name: "Next question", exact: true })
      .focus();
    await page.keyboard.press("Enter");
    await expect(page.locator("#question-heading")).toBeFocused();
    await page.request.post("/__test/provider", {
      data: { mode: "", delayMS: 0 },
    });
    await context.close();
  });
}
