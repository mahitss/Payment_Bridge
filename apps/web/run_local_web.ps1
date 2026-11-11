$env:API_INTERNAL_URL = "http://localhost:8080"
$env:NEXT_PUBLIC_DEMO_MODE = "true"
$env:PORT = "3000"

Write-Host "Starting ClaimBridge Next.js Web Frontend on http://localhost:3000..."
npm run dev
