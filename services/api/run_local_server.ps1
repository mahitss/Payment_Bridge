$env:PORT = "8080"
$env:CLAIMBRIDGE_ENV = "development"
$env:AI_ENABLED = "true"
$env:AI_PROVIDER = "ollama"
$env:AI_BASE_URL = "http://127.0.0.1:11434"
$env:AI_MODEL = "qwen3.5:4b"
$env:AI_REQUEST_TIMEOUT_SECONDS = "240"
$env:AI_MAX_OUTPUT_TOKENS = "2048"
$env:POLICY_ENGINE_BIN = "C:\Users\pc\OneDrive\Desktop\claim bridge\services\policy\target\release\claimbridge-policy.exe"

Write-Host "Starting ClaimBridge API Gateway with Qwen3.5 4B on port 8080..."
.\server.exe
