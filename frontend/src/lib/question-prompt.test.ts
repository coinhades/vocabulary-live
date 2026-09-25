import { describe, expect, it } from "vitest";
import { questionPromptParts } from "./question-prompt";

describe("question emphasis", () => {
  it.each([
    ["Which word is closest in meaning to abundant?", "abundant"],
    ["To reserve a room means to…", "reserve"],
    ["To clarify an instruction is to make it…", "clarify"],
    ["Your destination is the place where you…", "destination"],
    ["At an airport, a departure is a flight…", "departure"],
    ["If a train is delayed, it will leave…", "delayed"],
  ])("emphasizes the declared word in %s", (text, word) => {
    const parts = questionPromptParts(text, word);
    expect(parts.filter((p) => p.emphasized).map((p) => p.text)).toEqual([
      word,
    ]);
    expect(parts.map((p) => p.text).join("")).toBe(text);
  });

  it.each([
    [
      "Which word means to improve something by making small changes?",
      undefined,
    ],
    [
      "Which word describes a solution that is practical and possible?",
      undefined,
    ],
    ["A concise message is…", ""],
    ["To reserve a room means to…", "   "],
    ["A concise message is…", "reliable"],
    ["We reserved it to preserve it.", "reserve"],
    ["A scar on a cargo ship.", "car"],
    ["écar caré car\u0301 car2 car_name", "car"],
  ])("does not guess or emphasize a partial match in %s", (text, word) => {
    expect(questionPromptParts(text, word)).toEqual([
      { text, emphasized: false },
    ]);
  });

  it.each([
    [
      "‘Reserve’, RESERVE, and reserve?",
      "reserve",
      ["Reserve", "RESERVE", "reserve"],
    ],
    ["Check-in differs from checking in.", "check-in", ["Check-in"]],
    ["To take off is different from take offering.", "take off", ["take off"]],
    ["A CAFÉ beside the café.", "café", ["CAFÉ", "café"]],
    ["C++ differs from C, and a.b differs from axb.", "a.b", ["a.b"]],
    ["C++ differs from C.", "C++", ["C++"]],
    ["To reserve a room means to…", " reserve ", ["reserve"]],
  ])("preserves literal text and case in %s", (text, word, expected) => {
    const parts = questionPromptParts(text, word);
    expect(parts.filter((p) => p.emphasized).map((p) => p.text)).toEqual(
      expected,
    );
    expect(parts.map((p) => p.text).join("")).toBe(text);
  });
});
