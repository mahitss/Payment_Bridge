# ClaimBridge — Offline Demo Video Artifact & Submission Guide

This guide details the end-to-end lifecycle of the deterministic ClaimBridge offline demo recording: how it is automatically captured, where to download it from GitHub Actions, how to produce the optional voiceover using free tools, and how to publish the final video to satisfy PayPal AI Hackathon submission requirements.

---

## 1. How the Video Recording is Generated

The demo walkthrough video is deterministically captured by an automated Playwright browser test:
- **Test File:** [`apps/web/e2e/demo-recording.spec.ts`](../../apps/web/e2e/demo-recording.spec.ts)
- **Engine:** Playwright Chromium headless browser driving the real Next.js App Router frontend, Go API gateway, and PostgreSQL database.
- **Resolution & Viewport:** Exactly 1080p Desktop (`1920x1080`), 100% zoom, controlled viewport.
- **Codec & Format:** WebM (`video/webm`), VP8/VP9 standard browser recording container.
- **Pacing & Duration:** Deliberately paced across all 6 scenes to produce ~145–160 seconds of video (hard quality gate: 120.0s – 175.0s, safely under the 180s hackathon maximum).
- **Determinism:**
  - Operates purely on reproducible seeded evaluation fixtures (`operator@claimbridge.internal`, `officer@claimbridge.internal`, `DEMO-W9821`).
  - Zero external network requests to live PayPal servers or cloud LLM endpoints.
  - Zero mockups or slideshows: every screen and button interaction executes the genuine ClaimBridge web application.
- **Recorded Scenes:**
  1. **Scene A (00:00 – 00:25):** Landing page value proposition, Sandbox Prototype badge, and truthful Gateway Probe diagnostic card (distinguishing API health from PayPal authentication).
  2. **Scene B (00:25 – 00:55):** Operator authentication, integer cents intake, and dual-custody policy rule detection (`Amount > $250.00`).
  3. **Scene C (00:55 – 01:25):** Initiation of local operation (`AWAITING_APPROVAL`), proof that the initiating operator is blocked from self-approval (HTTP 403), officer login, and dual-custody approval (transitions to `APPROVED` without auto-submitting to PayPal).
  4. **Scene D (01:25 – 01:50):** Synthetic fixture provenance on `DEMO-W9821` with prominent **“SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION”** banner, synthetic identifiers, and disabled mutation buttons.
  5. **Scene E & F (01:50 – 02:35):** Grounded forensic AI investigation card with persisted findings, advisory notice, database snapshot citations, and architectural closing.

---

## 2. CI Pipeline Job, Quality Gate & Artifact Location

Every successful GitHub Actions CI execution against the Docker Compose evaluator stack enforces a strict media-integrity quality gate before uploading the artifact:

- **GitHub Repository:** [`https://github.com/mahitss/Payment_Bridge`](https://github.com/mahitss/Payment_Bridge)
- **CI Workflow File:** [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)
- **CI Job Name:** `Docker Compose Offline Evaluator Verification` (`docker-compose-evaluator`)
- **Media Quality Gate Script:** [`apps/web/scripts/verify-recording-artifact.mjs`](../../apps/web/scripts/verify-recording-artifact.mjs)
  - Deterministically provisions `ffprobe`.
  - Asserts resolution is exactly `1920x1080`.
  - Asserts measured duration is strictly within `120.0s – 175.0s`.
  - Asserts valid video stream exists and verifies audio track count (0 audio streams / silent).
  - Emits machine-readable JSON and renders formatted Markdown table to `$GITHUB_STEP_SUMMARY`.
- **Artifact Name:** `claimbridge-offline-demo-recording`
- **File Inside Artifact:** `claimbridge-demo-walkthrough.webm`
- **Retention Period:** 14 days

### Step-by-Step: Downloading the Artifact from GitHub Actions
1. Navigate to the ClaimBridge Actions tab: [https://github.com/mahitss/Payment_Bridge/actions](https://github.com/mahitss/Payment_Bridge/actions).
2. Click on the latest green workflow run on the `main` branch.
3. Scroll down to the bottom of the summary page to the **Artifacts** section.
4. Click on **`claimbridge-offline-demo-recording`** to download the zip bundle.
5. Extract the archive to access `claimbridge-demo-walkthrough.webm`.

---

## 3. Audio & Narration Status

> [!IMPORTANT]
> **Audio Status:** The raw generated `.webm` artifact is a **silent screen recording** captured directly by the browser context. It does not include an audio stream.
> 
> Voiceover narration and public video hosting remain an explicit human-in-the-loop production step before final hackathon submission.

---

## 4. Voiceover & Video Editing Guide (Free Tools)

You can record voiceover and edit the walkthrough clip using 100% free, open-source software:

### Recommended Free Tools:
1. **Windows Clipchamp** (Pre-installed on Windows 11) or **Shotcut** / **Kdenlive** (Cross-platform, open source).
2. **OBS Studio** (Open source): Can play the `.webm` video in a media source while you speak into your microphone and record the composite video.
3. **Audacity** (Open source): For recording high-quality voiceover audio tracks, noise reduction, and level normalization (-14 LUFS).

### Step-by-Step Voiceover Workflow:
1. Open the video in your editor of choice (e.g. Clipchamp, Shotcut, or DaVinci Resolve free edition).
2. Import the script from [`docs/demo/DEMO_SCRIPT.md`](DEMO_SCRIPT.md).
3. Read the script at a natural, measured pace aligned with the on-screen visual transitions:
   - **00:00 – 00:25:** Landing page, Sandbox Prototype badge, System Diagnostic Probe.
   - **00:25 – 00:55:** Operator login, case creation, $350 dual-custody mandate.
   - **00:55 – 01:25:** Operation initiation, self-approval blocked (HTTP 403), officer approval without auto-submit.
   - **01:25 – 01:50:** DEMO-W9821 synthetic fixture banner and disabled mutation buttons.
   - **01:50 – 02:20:** Forensic investigation panel, advisory disclaimer, grounded evidence citations.
   - **02:20 – 02:35:** Verified test suites, deterministic boundaries, conclusion.
4. Export the composite video as MP4 (`H.264 / AAC`, 1080p, 30fps or 60fps).

### Verifying Final Duration (< 3 Minutes):
The hackathon strictly mandates a **3-minute maximum (180 seconds)**:
- Verify duration using Windows Explorer (Properties -> Details) or via command line:
  ```bash
  ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 final_demo.mp4
  ```
- Ensure the exported duration is between **120 seconds and 175 seconds**.

---

## 5. Public Upload & Submission Status Tracking

A downloadable GitHub Actions artifact is **not** a publicly accessible URL for hackathon judges who cannot log into GitHub.

### Public Video Hosting Options:
1. **YouTube:** Upload as **Unlisted** or **Public**. Title: *"ClaimBridge — PayPal AI Hackathon 2026 Technical Demo"*.
2. **Vimeo:** Upload as Public.
3. **Loom / Google Drive:** If using Google Drive, ensure sharing permissions are set to *"Anyone with the link can view"*.

### Truthful Grounding & Description Requirement:
In the video description or pinned comment, include the following statement:
> *"This video demonstrates the reproducible offline evaluator walkthrough of ClaimBridge. Features shown operate on pre-seeded synthetic fixtures with fail-closed safety policies. Live PayPal Sandbox credentials and genuine Ollama model inference are independently verified through diagnostic tooling when configured."*

### Updating Repository Documentation:
Once the public URL is generated:
1. Open [`README.md`](../../README.md), [`docs/release/PROJECT_STATUS.md`](../release/PROJECT_STATUS.md), and [`docs/submission/VERIFICATION_MATRIX.md`](../submission/VERIFICATION_MATRIX.md).
2. Change the Demo Video status from `PENDING` to `PUBLISHED: [Video Link](https://...)`.
3. Provide the exact link in Devpost or Hackathon submission form.

---

## 6. Current Verification State Summary

| Component | Status | Location / Command |
| :--- | :---: | :--- |
| **Demo Walkthrough Code** | **VERIFIED** | `apps/web/e2e/demo-recording.spec.ts` |
| **CI Video Artifact Generation** | **VERIFIED** | GitHub Actions `docker-compose-evaluator` (`claimbridge-offline-demo-recording`) |
| **Demo Voiceover Script & Cues** | **VERIFIED** | `docs/demo/DEMO_SCRIPT.md` & `docs/demo/SHOT_LIST.md` |
| **PayPal Sandbox Credentials** | `BLOCKED` | Unset in `.env` (Zero real money moved) |
| **Genuine Ollama Inference** | `BLOCKED` | Local daemon offline during CI (Handled gracefully with HTTP 503) |
| **Public Submission Video URL** | `PENDING` | Awaiting team recording session and video upload |
