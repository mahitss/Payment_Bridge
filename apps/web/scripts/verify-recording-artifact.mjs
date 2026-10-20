#!/usr/bin/env node

import fs from 'fs';
import path from 'path';
import { execSync } from 'child_process';

// Resolve video path whether run from apps/web or repository root
function resolveVideoPath(userPath) {
  if (userPath) return path.resolve(userPath);

  const candidates = [
    path.resolve(process.cwd(), 'test-results', 'demo-recording', 'claimbridge-demo-walkthrough.webm'),
    path.resolve(process.cwd(), 'apps', 'web', 'test-results', 'demo-recording', 'claimbridge-demo-walkthrough.webm'),
    path.resolve(new URL('..', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1'), 'test-results', 'demo-recording', 'claimbridge-demo-walkthrough.webm'),
  ];

  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }

  // Fallback to primary candidate for error reporting
  return candidates[0];
}

const targetFile = resolveVideoPath(process.argv[2]);

console.log(`[Media Gate] Verifying demo recording artifact at: ${targetFile}`);

// 1. Check file existence
if (!fs.existsSync(targetFile)) {
  console.error(`[Media Gate: FAILED] Recording artifact does not exist: ${targetFile}`);
  process.exit(1);
}

// 2. Check file size
const stats = fs.statSync(targetFile);
if (stats.size < 10000) {
  console.error(`[Media Gate: FAILED] Recording file is too small or corrupt: ${stats.size} bytes`);
  process.exit(1);
}

// 3. Check for ffprobe availability
try {
  execSync('ffprobe -version', { stdio: 'ignore' });
} catch (err) {
  console.error('[Media Gate: FAILED] ffprobe executable is not available in PATH.');
  process.exit(1);
}

// 4. Query media metadata via ffprobe in JSON format
let metadata;
try {
  const probeOutput = execSync(
    `ffprobe -v error -show_entries stream=codec_type,codec_name,width,height -show_entries format=duration,size,format_name -of json "${targetFile}"`,
    { encoding: 'utf-8' }
  );
  metadata = JSON.parse(probeOutput);
} catch (err) {
  console.error(`[Media Gate: FAILED] ffprobe failed to inspect media: ${err.message}`);
  process.exit(1);
}

// 5. Extract streams and format details
const format = metadata.format || {};
const streams = metadata.streams || [];

const videoStreams = streams.filter((s) => s.codec_type === 'video');
const audioStreams = streams.filter((s) => s.codec_type === 'audio');

if (videoStreams.length === 0) {
  console.error('[Media Gate: FAILED] No video stream found in recorded WebM container.');
  process.exit(1);
}

const primaryVideo = videoStreams[0];
const width = parseInt(primaryVideo.width, 10);
const height = parseInt(primaryVideo.height, 10);
const videoCodec = primaryVideo.codec_name || 'unknown';

const duration = parseFloat(format.duration || '0');
const sizeBytes = parseInt(format.size || stats.size.toString(), 10);

const hasAudio = audioStreams.length > 0;
const audioCodec = hasAudio ? audioStreams[0].codec_name : 'none';

// 6. Assertions against hackathon requirements
const MIN_DURATION_SEC = 120.0;
const MAX_DURATION_SEC = 175.0;
const TARGET_WIDTH = 1920;
const TARGET_HEIGHT = 1080;

const errors = [];

if (width !== TARGET_WIDTH || height !== TARGET_HEIGHT) {
  errors.push(
    `Video resolution is ${width}x${height}; expected exactly ${TARGET_WIDTH}x${TARGET_HEIGHT} (1080p).`
  );
}

if (duration < MIN_DURATION_SEC || duration > MAX_DURATION_SEC) {
  errors.push(
    `Measured duration is ${duration.toFixed(2)}s; must be between ${MIN_DURATION_SEC}s and ${MAX_DURATION_SEC}s (under 3-minute hackathon limit).`
  );
}

if (hasAudio) {
  console.warn(
    `[Media Gate: NOTICE] Audio track detected (${audioCodec}). Note: automated recordings are normally silent.`
  );
}

// 7. Output machine-readable JSON summary
const summary = {
  file: path.relative(process.cwd(), targetFile),
  sizeBytes,
  durationSeconds: parseFloat(duration.toFixed(2)),
  width,
  height,
  videoCodec,
  audioTrackCount: audioStreams.length,
  hasAudio,
  minDurationSec: MIN_DURATION_SEC,
  maxDurationSec: MAX_DURATION_SEC,
  passed: errors.length === 0,
  errors,
};

console.log('[Media Gate: RESULT]');
console.log(JSON.stringify(summary, null, 2));

// 8. Render GitHub Actions Step Summary if available
if (process.env.GITHUB_STEP_SUMMARY) {
  const summaryMarkdown = [
    '### Demo Recording Media-Integrity Verification',
    '',
    `**Evaluation Outcome:** ${errors.length === 0 ? '✅ **PASSED**' : '❌ **FAILED**'}`,
    '',
    '| Metric | Verified Value | Requirement / Boundary | Status |',
    '| :--- | :--- | :--- | :--- |',
    `| **File Location** | \`${summary.file}\` | Present on disk | ${stats.size > 0 ? '✅ OK' : '❌ MISSING'} |`,
    `| **File Size** | ${sizeBytes.toLocaleString()} bytes (~${(sizeBytes / 1024 / 1024).toFixed(2)} MB) | > 10,000 bytes | ${sizeBytes > 10000 ? '✅ OK' : '❌ TOO SMALL'} |`,
    `| **Dimensions** | ${width} × ${height} | Exactly 1920 × 1080 (1080p) | ${width === 1920 && height === 1080 ? '✅ OK' : '❌ MISMATCH'} |`,
    `| **Measured Duration** | ${duration.toFixed(2)}s (${Math.floor(duration / 60)}m ${Math.floor(duration % 60)}s) | 120.0s – 175.0s (under 180s limit) | ${duration >= MIN_DURATION_SEC && duration <= MAX_DURATION_SEC ? '✅ OK' : '❌ OUT OF RANGE'} |`,
    `| **Video Stream** | ${videoCodec} | Valid WebM video stream | ✅ OK |`,
    `| **Audio Stream** | ${hasAudio ? audioCodec : '0 streams (Silent browser capture)'} | Silent browser capture (voiceover pending) | ✅ OK |`,
    '',
  ];

  if (errors.length > 0) {
    summaryMarkdown.push('#### Verification Errors:');
    for (const err of errors) {
      summaryMarkdown.push(`- ❌ ${err}`);
    }
  } else {
    summaryMarkdown.push('> [!NOTE]');
    summaryMarkdown.push('> **Media Contract Verified:** The walkthrough video artifact matches 1080p resolution and falls squarely within the 120s–175s hackathon window. The recording is ready for human voiceover production.');
  }

  try {
    fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, summaryMarkdown.join('\n') + '\n');
  } catch (writeErr) {
    console.warn(`Could not write to GITHUB_STEP_SUMMARY: ${writeErr.message}`);
  }
}

if (errors.length > 0) {
  console.error('[Media Gate: FAILED] Quality gate violations:');
  for (const err of errors) {
    console.error(` - ${err}`);
  }
  process.exit(1);
}

console.log('[Media Gate: SUCCESS] All video media-integrity requirements satisfied!');
process.exit(0);
