import { test, expect } from "@playwright/test";
import {
  getInvestigationTransportInfo,
  formatInferenceDuration,
  formatElapsedTimer,
} from "../src/lib/payouts";

test.describe("Mission 025: Transparency Helper Unit Tests", () => {
  test("identifies native Ollama chat transport truthfully", () => {
    const info = getInvestigationTransportInfo({
      raw_response: { transport: "ollama_native_chat" },
    } as any);
    expect(info.type).toBe("native_ollama");
    expect(info.label).toBe("Native Ollama Chat");
    expect(info.badgeLabel).toBe("Ollama Native Chat");
  });

  test("identifies OpenAI-compatible chat transport truthfully", () => {
    const info = getInvestigationTransportInfo({
      raw_response: { transport: "openai_compatible_chat" },
    } as any);
    expect(info.type).toBe("openai_compatible");
    expect(info.label).toBe("OpenAI-Compatible Chat");
    expect(info.badgeLabel).toBe("OpenAI Compatible");
  });

  test("identifies historical records without transport and never infers from model name", () => {
    const info = getInvestigationTransportInfo({
      model: "qwen3.5:4b", // Model name must NOT cause native inference
      provider: "ollama-openai-compatible",
      raw_response: { model: "qwen3.5:4b" }, // No transport field
    } as any);
    expect(info.type).toBe("historical");
    expect(info.label).toBe("Standard Provider (Historical / Untracked Transport)");
    expect(info.badgeLabel).toBe("Historical / Untracked");
  });

  test("formats duration accurately when non-negative metadata exists", () => {
    expect(formatInferenceDuration({ inference_duration_ms: 60600 })).toBe("60.6s (60,600ms)");
    expect(formatInferenceDuration({ inference_duration_ms: 1250 })).toBe("1.3s (1,250ms)");
    expect(formatInferenceDuration({ inference_duration_ms: 500 })).toBe("500ms");
    expect(formatInferenceDuration({ inference_duration_ms: 0 })).toBe("0ms");
  });

  test("returns null for historical records without duration to prevent invented values", () => {
    expect(formatInferenceDuration(undefined)).toBeNull();
    expect(formatInferenceDuration(null)).toBeNull();
    expect(formatInferenceDuration({})).toBeNull();
    expect(formatInferenceDuration({ inference_duration_ms: undefined })).toBeNull();
    expect(formatInferenceDuration({ inference_duration_ms: -100 })).toBeNull();
    expect(formatInferenceDuration({ inference_duration_ms: "60.6s" as any })).toBeNull();
  });

  test("formats elapsed timer for in-progress counter", () => {
    expect(formatElapsedTimer(0)).toBe("0s");
    expect(formatElapsedTimer(45)).toBe("45s");
    expect(formatElapsedTimer(60)).toBe("1m 00s");
    expect(formatElapsedTimer(61)).toBe("1m 01s");
    expect(formatElapsedTimer(125)).toBe("2m 05s");
  });
});
