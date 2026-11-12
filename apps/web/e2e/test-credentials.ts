// apps/web/e2e/test-credentials.ts
// Rotated local evaluation credentials. Evaluators configure these locally via DEMO_OPERATOR_PASSWORD / DEMO_OFFICER_PASSWORD.

export const DEMO_OPERATOR_PASSWORD =
  process.env.DEMO_OPERATOR_PASSWORD || "ClaimBridge-Op-2026!Eval";

export const DEMO_OFFICER_PASSWORD =
  process.env.DEMO_OFFICER_PASSWORD || "ClaimBridge-Off-2026!Eval";
