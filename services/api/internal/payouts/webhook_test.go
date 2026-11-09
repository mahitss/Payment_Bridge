package payouts

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
)

type webhookTestEnv struct {
	cfg      *config.Config
	repo     *db.MockRepository
	service  *Service
	handler  *Handler
	cert     *x509.Certificate
	privKey  *rsa.PrivateKey
	certURL  string
	fetcher  *paypal.MockCertFetcher
	verifier *paypal.WebhookVerifier
}

func setupWebhookTestEnv(t *testing.T) *webhookTestEnv {
	t.Helper()

	now := time.Now().UTC()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed generating rsa key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(4242),
		Subject: pkix.Name{
			CommonName:   "api.sandbox.paypal.com",
			Organization: []string{"PayPal, Inc."},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("failed creating x509 cert: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed parsing cert: %v", err)
	}

	certURL := "https://api.sandbox.paypal.com/v1/notifications/certs/CERT-WH-TEST.pem"
	fetcher := &paypal.MockCertFetcher{
		Certs: map[string]*x509.Certificate{
			certURL: cert,
		},
	}
	verifier := paypal.NewWebhookVerifier(fetcher)

	cfg := &config.Config{
		AppEnv:               "development",
		PayPalEnv:            "sandbox",
		PayPalBaseURL:        config.OfficialPayPalSandboxBaseURL,
		PayPalWebhookID:      "WH-TEST-SANDBOX-12345",
		PayPalRequestTimeout: 5 * time.Second,
	}

	repo := db.NewMockRepository()
	service := NewService(cfg, repo, nil, nil)
	service.SetWebhookVerifier(verifier)
	handler := NewHandler(cfg, service)

	return &webhookTestEnv{
		cfg:      cfg,
		repo:     repo,
		service:  service,
		handler:  handler,
		cert:     cert,
		privKey:  privKey,
		certURL:  certURL,
		fetcher:  fetcher,
		verifier: verifier,
	}
}

func (env *webhookTestEnv) sign(t *testing.T, transmissionID, transmissionTime string, rawBody []byte) (paypal.WebhookHeaders, error) {
	t.Helper()
	sig, err := paypal.SignWebhookPayload(env.privKey, transmissionID, transmissionTime, env.cfg.PayPalWebhookID, rawBody)
	if err != nil {
		return paypal.WebhookHeaders{}, err
	}
	return paypal.WebhookHeaders{
		TransmissionID:   transmissionID,
		TransmissionTime: transmissionTime,
		CertURL:          env.certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sig,
	}, nil
}

func (env *webhookTestEnv) createTestOperation(t *testing.T, orgID, batchID, itemID, senderBatchID, senderItemID string, synthetic bool) (*db.PayoutCase, *db.PayoutOperation) {
	t.Helper()
	pCase := &db.PayoutCase{
		ID:                 crypto.GenerateUUID(),
		OrganizationID:     orgID,
		CreatedByUserID:    "user-1",
		Reference:          "REF-" + crypto.GenerateUUID()[:8],
		RecipientEmail:     "recipient@example.com",
		RecipientName:      "Test Recipient",
		Title:              "Settlement Claim",
		Currency:           "USD",
		AmountCents:        10000,
		Status:             "SUBMITTED",
		IsSyntheticFixture: synthetic,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	_ = env.repo.CreatePayoutCase(context.Background(), pCase)

	batchIDPtr := &batchID
	itemIDPtr := &itemID
	op := &db.PayoutOperation{
		ID:                  crypto.GenerateUUID(),
		PayoutCaseID:        pCase.ID,
		OrganizationID:      orgID,
		InitiatedByUserID:   "user-1",
		SenderBatchID:       senderBatchID,
		SenderItemID:        senderItemID,
		AmountCents:         10000,
		Currency:            "USD",
		Status:              "SUBMITTED",
		PayPalPayoutBatchID: batchIDPtr,
		PayPalPayoutItemID:  itemIDPtr,
		IsSyntheticFixture:  synthetic,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
	_ = env.repo.CreatePayoutOperation(context.Background(), op)
	return pCase, op
}

func TestWebhook_ValidSignatureAcceptance_ItemSuccess(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-PP-001"
	itemID := "ITEM-PP-001"
	pCase, op := env.createTestOperation(t, orgID, batchID, itemID, "SB-001", "SI-001", false)

	payload := map[string]interface{}{
		"id":            "WH-EVT-001",
		"event_version": "1.0",
		"create_time":   "2026-11-07T02:00:00Z",
		"resource_type": "payouts_item",
		"event_type":    paypal.EventPayoutsItemSucceeded,
		"summary":       "Item transaction completed",
		"resource": map[string]interface{}{
			"payout_item_id":     itemID,
			"payout_batch_id":    batchID,
			"transaction_id":     "TX-PP-001",
			"transaction_status": "SUCCESS",
			"sender_item_id":     "SI-001",
		},
	}
	rawBody, _ := json.Marshal(payload)

	transID := "trans-001"
	transTime := "2026-11-07T02:00:05Z"
	headers, err := env.sign(t, transID, transTime, rawBody)
	if err != nil {
		t.Fatalf("failed signing payload: %v", err)
	}

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("expected successful webhook processing, got: %v", err)
	}

	if res.IsDuplicate {
		t.Errorf("expected IsDuplicate = false")
	}
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected event status PROCESSED, got %s", res.Event.ProcessingStatus)
	}

	// Verify database operation was transitioned to SUCCEEDED
	updatedOp, err := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if err != nil {
		t.Fatalf("failed fetching updated operation: %v", err)
	}
	if updatedOp.Status != "SUCCEEDED" {
		t.Errorf("expected operation status SUCCEEDED, got %s", updatedOp.Status)
	}
	if updatedOp.CompletedAt == nil {
		t.Errorf("expected operation CompletedAt to be non-nil")
	}

	// Verify case was completed
	updatedCase, err := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if err != nil {
		t.Fatalf("failed fetching updated case: %v", err)
	}
	if updatedCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED, got %s", updatedCase.Status)
	}

	// Verify status snapshot recorded
	snapshots, err := env.repo.ListPayoutStatusSnapshots(ctx, orgID, op.ID)
	if err != nil || len(snapshots) == 0 {
		t.Fatalf("expected status snapshot to be recorded, got %v", err)
	}
	if *snapshots[0].ItemStatus != "SUCCESS" {
		t.Errorf("expected snapshot item status SUCCESS, got %v", snapshots[0].ItemStatus)
	}
}

func TestWebhook_InvalidSignature_Rejection(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-PP-002", "ITEM-PP-002", "SB-002", "SI-002", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-002",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id": "ITEM-PP-002",
		},
	}
	rawBody, _ := json.Marshal(payload)

	headers, _ := env.sign(t, "trans-002", "2026-11-07T02:00:00Z", rawBody)
	// Corrupt signature
	headers.TransmissionSig = "aW52YWxpZC1zaWduYXR1cmU="

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err == nil {
		t.Fatalf("expected error for invalid signature, got nil")
	}
	if res != nil {
		t.Errorf("expected nil result on invalid signature, got %v", res)
	}

	// Verify operation status remained SUBMITTED
	unchangedOp, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if unchangedOp.Status != "SUBMITTED" {
		t.Errorf("operation status must not change on invalid signature, got: %s", unchangedOp.Status)
	}

	// Verify durable record logged as FAILED_SIGNATURE
	unverified, err := env.repo.GetWebhookEventByPayPalEventID(ctx, "WH-EVT-002")
	if err != nil {
		t.Fatalf("expected unverified event record, got error: %v", err)
	}
	if unverified.SignatureVerified {
		t.Errorf("expected SignatureVerified = false")
	}
	if unverified.ProcessingStatus != "FAILED_SIGNATURE" {
		t.Errorf("expected processing status FAILED_SIGNATURE, got %s", unverified.ProcessingStatus)
	}
}

func TestWebhook_MissingSignatureHeaders_HTTP(t *testing.T) {
	env := setupWebhookTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/paypal/webhooks", bytes.NewReader([]byte(`{"id":"WH-1"}`)))
	req.Header.Set("Content-Type", "application/json")
	// Missing PAYPAL-TRANSMISSION-ID, etc.

	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	env.handler.RegisterRoutes(mux)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for missing headers, got %d", w.Code)
	}
}

func TestWebhook_InvalidCertURL_SSRFProtection(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	rawBody := []byte(`{"id":"WH-EVT-SSRF","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)
	headers := paypal.WebhookHeaders{
		TransmissionID:   "trans-ssrf",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          "https://evil-paypal.com/attacker.pem",
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "c2ln",
	}

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err == nil {
		t.Fatalf("expected SSRF error for evil-paypal.com, got nil")
	}
}

func TestWebhook_MalformedJSON(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	malformed := []byte(`{"id": "WH-BROKEN", "event_type":`)
	headers, _ := env.sign(t, "trans-malformed", "2026-11-07T02:00:00Z", malformed)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, malformed)
	if err == nil {
		t.Fatalf("expected malformed JSON error, got nil")
	}
}

func TestWebhook_OversizedPayload_Rejection_HTTP(t *testing.T) {
	env := setupWebhookTestEnv(t)

	// Create 600KB payload (exceeding 512KB limit)
	hugePayload := bytes.Repeat([]byte("a"), 600*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/paypal/webhooks", bytes.NewReader(hugePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PAYPAL-TRANSMISSION-ID", "huge-1")
	req.Header.Set("PAYPAL-TRANSMISSION-TIME", "2026-11-07T02:00:00Z")
	req.Header.Set("PAYPAL-CERT-URL", env.certURL)
	req.Header.Set("PAYPAL-AUTH-ALGO", "SHA256withRSA")
	req.Header.Set("PAYPAL-TRANSMISSION-SIG", "c2ln")

	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	env.handler.RegisterRoutes(mux)
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for oversized payload, got %d", w.Code)
	}
}

func TestWebhook_MissingWebhookID_Rejection(t *testing.T) {
	env := setupWebhookTestEnv(t)
	env.cfg.PayPalWebhookID = "" // Webhook ID unconfigured

	rawBody := []byte(`{"id":"WH-NOWEBHOOKID"}`)
	headers := paypal.WebhookHeaders{
		TransmissionID:   "t-1",
		TransmissionTime: "2026-11-07T02:00:00Z",
		CertURL:          env.certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  "c2ln",
	}

	_, err := env.service.ProcessPayPalWebhook(context.Background(), headers, rawBody)
	if err != ErrWebhookUnconfigured {
		t.Fatalf("expected ErrWebhookUnconfigured, got %v", err)
	}
}

func TestWebhook_StorageNotDurable_SandboxRejected_HTTP(t *testing.T) {
	env := setupWebhookTestEnv(t)
	// Change environment to sandbox
	env.cfg.AppEnv = "sandbox"

	rawBody := []byte(`{"id":"WH-STAGING-TEST","event_type":"PAYMENT.PAYOUTS-ITEM.SUCCEEDED"}`)
	headers, err := env.sign(t, "t-staging-1", "2026-11-07T02:00:00Z", rawBody)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/paypal/webhooks", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PAYPAL-TRANSMISSION-ID", headers.TransmissionID)
	req.Header.Set("PAYPAL-TRANSMISSION-TIME", headers.TransmissionTime)
	req.Header.Set("PAYPAL-CERT-URL", headers.CertURL)
	req.Header.Set("PAYPAL-AUTH-ALGO", headers.AuthAlgo)
	req.Header.Set("PAYPAL-TRANSMISSION-SIG", headers.TransmissionSig)

	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	env.handler.RegisterRoutes(mux)
	mux.ServeHTTP(w, req)

	// Since env.repo is db.MockRepository (driver: mock_memory), it must be rejected with 503
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 when persistent PostgreSQL storage is unavailable in sandbox mode, got %d", w.Code)
	}

	var respBody map[string]string
	_ = json.NewDecoder(w.Body).Decode(&respBody)
	if respBody["code"] != "STORAGE_NOT_DURABLE" {
		t.Errorf("expected code STORAGE_NOT_DURABLE, got %v", respBody["code"])
	}
}

func TestWebhook_DuplicateDeliveries_Idempotency(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-DUP-1"
	itemID := "ITEM-DUP-1"
	_, op := env.createTestOperation(t, orgID, batchID, itemID, "SB-DUP", "SI-DUP", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-DUP-001",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     itemID,
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "trans-dup", "2026-11-07T02:00:00Z", rawBody)

	// First delivery
	res1, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("first delivery failed: %v", err)
	}
	if res1.IsDuplicate {
		t.Errorf("first delivery should not be duplicate")
	}

	// Second delivery of the identical event
	res2, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("second delivery failed: %v", err)
	}
	if !res2.IsDuplicate {
		t.Errorf("second delivery must be flagged as duplicate")
	}

	// Verify operation status was not double-mutated or corrupted
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected op to remain SUCCEEDED, got %s", opAfter.Status)
	}
}

func TestWebhook_ConcurrentDuplicateDeliveries(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-CONC-1"
	itemID := "ITEM-CONC-1"
	env.createTestOperation(t, orgID, batchID, itemID, "SB-CONC", "SI-CONC", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-CONC-001",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     itemID,
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "trans-conc", "2026-11-07T02:00:00Z", rawBody)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	results := make([]*WebhookProcessResult, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("routine %d encountered error: %v", i, err)
		}
	}

	// Exactly one should be initial, one should be duplicate (or both succeed gracefully)
	dups := 0
	for _, res := range results {
		if res.IsDuplicate {
			dups++
		}
	}
	if dups < 1 {
		t.Errorf("expected at least one duplicate detection in concurrent delivery")
	}
}

func TestWebhook_BatchStatusProcessing_Denied(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-DENIED-1"
	pCase, op := env.createTestOperation(t, orgID, batchID, "ITEM-DENIED", "SB-DENIED", "SI-DENIED", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-DENIED",
		"event_type": paypal.EventPayoutsBatchDenied,
		"resource": map[string]interface{}{
			"batch_header": map[string]interface{}{
				"payout_batch_id": batchID,
				"batch_status":    "DENIED",
			},
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "trans-denied", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("processing batch denied failed: %v", err)
	}

	updatedOp, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if updatedOp.Status != "FAILED" {
		t.Errorf("expected operation FAILED on batch DENIED, got %s", updatedOp.Status)
	}
	updatedCase, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if updatedCase.Status != "DENIED" {
		t.Errorf("expected case DENIED on batch DENIED, got %s", updatedCase.Status)
	}
}

func TestWebhook_BatchStatusProcessing_Processing(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-PROC-1"
	_, op := env.createTestOperation(t, orgID, batchID, "ITEM-PROC", "SB-PROC", "SI-PROC", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-PROC",
		"event_type": paypal.EventPayoutsBatchProcessing,
		"resource": map[string]interface{}{
			"batch_header": map[string]interface{}{
				"payout_batch_id": batchID,
				"batch_status":    "PROCESSING",
			},
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "trans-proc", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("processing batch processing failed: %v", err)
	}

	updatedOp, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if updatedOp.Status != "SUBMITTED" {
		t.Errorf("expected operation SUBMITTED, got %s", updatedOp.Status)
	}
	if *updatedOp.PayPalBatchStatus != "PROCESSING" {
		t.Errorf("expected PayPalBatchStatus PROCESSING, got %s", *updatedOp.PayPalBatchStatus)
	}
}

func TestWebhook_BatchSuccessWithoutItemSuccess_Invariants(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-SUCCESS-EMPTY-ITEMS"
	pCase, op := env.createTestOperation(t, orgID, batchID, "ITEM-1", "SB-SUCCESS", "SI-1", false)

	// Batch SUCCESS event with NO items in notification payload
	payload := map[string]interface{}{
		"id":         "WH-EVT-BATCH-SUCCESS",
		"event_type": paypal.EventPayoutsBatchSuccess,
		"resource": map[string]interface{}{
			"batch_header": map[string]interface{}{
				"payout_batch_id": batchID,
				"batch_status":    "SUCCESS",
			},
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "trans-batch-succ", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("processing batch success failed: %v", err)
	}

	updatedOp, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	// CRITICAL INVARIANT: Operation must NOT be SUCCEEDED; must remain SUBMITTED pending item proof
	if updatedOp.Status == "SUCCEEDED" {
		t.Fatalf("VIOLATION: Operation was prematurely marked SUCCEEDED without item-authoritative proof!")
	}
	if updatedOp.Status != "SUBMITTED" {
		t.Errorf("expected operation to remain SUBMITTED, got %s", updatedOp.Status)
	}

	updatedCase, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if updatedCase.Status == "COMPLETED" {
		t.Fatalf("VIOLATION: Case was prematurely marked COMPLETED without item-authoritative proof!")
	}
}

func TestWebhook_ItemStatusProcessing_Failed_And_Blocked(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	// 1. Failed
	orgID := "org-1"
	pCase1, op1 := env.createTestOperation(t, orgID, "BATCH-F", "ITEM-F", "SB-F", "SI-F", false)
	payload1 := map[string]interface{}{
		"id":         "WH-EVT-FAILED",
		"event_type": paypal.EventPayoutsItemFailed,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-F",
			"transaction_status": "FAILED",
		},
	}
	rawBody1, _ := json.Marshal(payload1)
	headers1, _ := env.sign(t, "t-f", "2026-11-07T02:00:00Z", rawBody1)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers1, rawBody1)
	if err != nil {
		t.Fatalf("failed processing item failed event: %v", err)
	}
	op1Updated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op1.ID)
	if op1Updated.Status != "FAILED" {
		t.Errorf("expected op status FAILED, got %s", op1Updated.Status)
	}
	case1Updated, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase1.ID)
	if case1Updated.Status != "FAILED" {
		t.Errorf("expected case status FAILED, got %s", case1Updated.Status)
	}

	// 2. Blocked
	pCase2, op2 := env.createTestOperation(t, orgID, "BATCH-B", "ITEM-B", "SB-B", "SI-B", false)
	payload2 := map[string]interface{}{
		"id":         "WH-EVT-BLOCKED",
		"event_type": paypal.EventPayoutsItemBlocked,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-B",
			"transaction_status": "BLOCKED",
		},
	}
	rawBody2, _ := json.Marshal(payload2)
	headers2, _ := env.sign(t, "t-b", "2026-11-07T02:00:00Z", rawBody2)

	_, err = env.service.ProcessPayPalWebhook(ctx, headers2, rawBody2)
	if err != nil {
		t.Fatalf("failed processing item blocked event: %v", err)
	}
	op2Updated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op2.ID)
	if op2Updated.Status != "FAILED" {
		t.Errorf("expected op status FAILED, got %s", op2Updated.Status)
	}
	case2Updated, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase2.ID)
	if case2Updated.Status != "FAILED" {
		t.Errorf("expected case status FAILED, got %s", case2Updated.Status)
	}
}

func TestWebhook_ItemStatusProcessing_Unclaimed(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-U", "ITEM-U", "SB-U", "SI-U", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-UNCLAIMED",
		"event_type": paypal.EventPayoutsItemUnclaimed,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-U",
			"transaction_status": "UNCLAIMED",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-u", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("failed processing unclaimed: %v", err)
	}

	opUpdated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opUpdated.Status != "SUBMITTED" {
		t.Errorf("unclaimed item must keep status SUBMITTED, got %s", opUpdated.Status)
	}
	if *opUpdated.PayPalItemStatus != "UNCLAIMED" {
		t.Errorf("expected PayPalItemStatus UNCLAIMED, got %s", *opUpdated.PayPalItemStatus)
	}
}

func TestWebhook_ItemStatusProcessing_Held_And_Refunded(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-H", "ITEM-H", "SB-H", "SI-H", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-HELD",
		"event_type": paypal.EventPayoutsItemHeld,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-H",
			"transaction_status": "ONHOLD",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-h", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("failed processing held event: %v", err)
	}

	opUpdated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opUpdated.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("held item must require reconciliation, got %s", opUpdated.Status)
	}
}

func TestWebhook_ItemStatusProcessing_Returned_And_Canceled(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	// Case A: Operation was previously CANCELLED by ClaimBridge
	_, opCancelled := env.createTestOperation(t, orgID, "BATCH-C1", "ITEM-C1", "SB-C1", "SI-C1", false)
	opCancelled.Status = "CANCELLED"
	_ = env.repo.UpdatePayoutOperation(ctx, opCancelled)

	payloadA := map[string]interface{}{
		"id":         "WH-EVT-RET-A",
		"event_type": paypal.EventPayoutsItemReturned,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-C1",
			"transaction_status": "RETURNED",
		},
	}
	rawBodyA, _ := json.Marshal(payloadA)
	headersA, _ := env.sign(t, "t-c1", "2026-11-07T02:00:00Z", rawBodyA)

	_, err := env.service.ProcessPayPalWebhook(ctx, headersA, rawBodyA)
	if err != nil {
		t.Fatalf("failed processing returned: %v", err)
	}
	opAUpdated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, opCancelled.ID)
	if opAUpdated.Status != "CANCELLED" {
		t.Errorf("already CANCELLED operation must stay CANCELLED, got %s", opAUpdated.Status)
	}

	// Case B: Operation was NOT cancelled by ClaimBridge
	_, opSubmitted := env.createTestOperation(t, orgID, "BATCH-C2", "ITEM-C2", "SB-C2", "SI-C2", false)
	payloadB := map[string]interface{}{
		"id":         "WH-EVT-RET-B",
		"event_type": paypal.EventPayoutsItemReturned,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-C2",
			"transaction_status": "RETURNED",
		},
	}
	rawBodyB, _ := json.Marshal(payloadB)
	headersB, _ := env.sign(t, "t-c2", "2026-11-07T02:00:00Z", rawBodyB)

	_, err = env.service.ProcessPayPalWebhook(ctx, headersB, rawBodyB)
	if err != nil {
		t.Fatalf("failed processing returned: %v", err)
	}
	opBUpdated, _ := env.repo.GetPayoutOperationByID(ctx, orgID, opSubmitted.ID)
	if opBUpdated.Status != "RECONCILIATION_REQUIRED" {
		t.Errorf("untracked return must trigger RECONCILIATION_REQUIRED, got %s", opBUpdated.Status)
	}
}

func TestWebhook_SyntheticFixtureProtection(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	// Create synthetic fixture operation
	_, synthOp := env.createTestOperation(t, orgID, "BATCH-SYNTH", "ITEM-SYNTH", "SB-SYNTH", "SI-SYNTH", true)

	payload := map[string]interface{}{
		"id":         "WH-EVT-SYNTH",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-SYNTH",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-synth", "2026-11-07T02:00:00Z", rawBody)

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("fixture webhook processing failed: %v", err)
	}

	if res.Event.ProcessingStatus != "IGNORED" {
		t.Errorf("expected status IGNORED on synthetic fixture, got %s", res.Event.ProcessingStatus)
	}

	// Verify operation status was completely untouched
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, synthOp.ID)
	if opAfter.Status != "SUBMITTED" {
		t.Errorf("synthetic fixture operation must never be mutated by external webhook, got status: %s", opAfter.Status)
	}
}

func TestWebhook_CrossTenantIsolation(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	// Org A has an operation
	_, opA := env.createTestOperation(t, "org-A", "BATCH-A", "ITEM-A", "SB-A", "SI-A", false)
	// Org B has an operation
	_, opB := env.createTestOperation(t, "org-B", "BATCH-B", "ITEM-B", "SB-B", "SI-B", false)

	// Webhook targeted for Org A's item
	payload := map[string]interface{}{
		"id":         "WH-EVT-ORGA",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-A",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-org", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	// Verify Org A is mutated
	updatedA, _ := env.repo.GetPayoutOperationByID(ctx, "org-A", opA.ID)
	if updatedA.Status != "SUCCEEDED" {
		t.Errorf("expected Org A operation SUCCEEDED, got %s", updatedA.Status)
	}

	// Verify Org B is completely unaffected
	updatedB, _ := env.repo.GetPayoutOperationByID(ctx, "org-B", opB.ID)
	if updatedB.Status != "SUBMITTED" {
		t.Errorf("expected Org B operation SUBMITTED (untouched), got %s", updatedB.Status)
	}
}

func TestWebhook_UnknownEventType_SafeHandling(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-UNK", "ITEM-UNK", "SB-UNK", "SI-UNK", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-UNKNOWN",
		"event_type": "PAYMENT.PAYOUTS.UNKNOWN_EXPERIMENTAL_EVENT",
		"resource": map[string]interface{}{
			"payout_batch_id": "BATCH-UNK",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-unk", "2026-11-07T02:00:00Z", rawBody)

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("expected unknown event to be safely handled, got error: %v", err)
	}
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected processing status PROCESSED, got %s", res.Event.ProcessingStatus)
	}

	// Verify operation status was NOT mutated
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUBMITTED" {
		t.Errorf("unknown event must not mutate operation status, got %s", opAfter.Status)
	}
}

func TestWebhook_CrashRecovery_ResumesReceivedEvent(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	batchID := "BATCH-CRASH-1"
	itemID := "ITEM-CRASH-1"
	pCase, op := env.createTestOperation(t, orgID, batchID, itemID, "SB-CRASH", "SI-CRASH", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-CRASH-001",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     itemID,
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-crash", "2026-11-07T02:00:00Z", rawBody)

	// Simulate server crash: event was written as RECEIVED into DB, but server crashed before business processing
	var rawSnapshot map[string]interface{}
	_ = json.Unmarshal(rawBody, &rawSnapshot)
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:     "WH-EVT-CRASH-001",
		EventType:         paypal.EventPayoutsItemSucceeded,
		ResourceType:      "payouts_item",
		TransmissionID:    headers.TransmissionID,
		WebhookID:         env.cfg.PayPalWebhookID,
		SignatureVerified: true,
		ProcessingStatus:  "RECEIVED",
		PayloadSnapshot:   rawSnapshot,
		CreatedAt:         time.Now().UTC().Add(-2 * time.Minute),
	})

	// Redelivery arrives from PayPal
	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("redelivery should resume and succeed, got error: %v", err)
	}

	// CRITICAL CHECK: Event must NOT be permanently stuck in RECEIVED!
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected status PROCESSED after crash recovery redelivery, got: %s", res.Event.ProcessingStatus)
	}

	// Payout operation MUST be reconciled
	updatedOp, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if updatedOp.Status != "SUCCEEDED" {
		t.Errorf("expected op status SUCCEEDED after redelivery recovery, got %s", updatedOp.Status)
	}
	updatedCase, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if updatedCase.Status != "COMPLETED" {
		t.Errorf("expected case status COMPLETED after redelivery recovery, got %s", updatedCase.Status)
	}

	// Subsequent (third) delivery: event is now PROCESSED, so it is safely ignored as duplicate
	resDup, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("third delivery failed: %v", err)
	}
	if !resDup.IsDuplicate {
		t.Errorf("third delivery must be flagged as duplicate")
	}
}

func TestWebhook_CrashRecovery_BackgroundWorker(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op1 := env.createTestOperation(t, orgID, "BATCH-RCV-1", "ITEM-RCV-1", "SB-R1", "SI-R1", false)
	_, op2 := env.createTestOperation(t, orgID, "BATCH-RCV-2", "ITEM-RCV-2", "SB-R2", "SI-R2", false)

	// Create 2 events stuck in RECEIVED
	p1 := map[string]interface{}{
		"id":         "WH-EVT-PEND-1",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-RCV-1",
			"transaction_status": "SUCCESS",
		},
	}
	p2 := map[string]interface{}{
		"id":         "WH-EVT-PEND-2",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-RCV-2",
			"transaction_status": "SUCCESS",
		},
	}
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:    "WH-EVT-PEND-1",
		EventType:        paypal.EventPayoutsItemSucceeded,
		ProcessingStatus: "RECEIVED",
		PayloadSnapshot:  p1,
		CreatedAt:        time.Now().UTC().Add(-5 * time.Minute),
	})
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:    "WH-EVT-PEND-2",
		EventType:        paypal.EventPayoutsItemSucceeded,
		ProcessingStatus: "RECEIVED",
		PayloadSnapshot:  p2,
		CreatedAt:        time.Now().UTC().Add(-5 * time.Minute),
	})

	// Run recovery worker
	recovered, err := env.service.RecoverPendingWebhookEvents(ctx, 1*time.Minute, 10)
	if err != nil {
		t.Fatalf("recovery worker failed: %v", err)
	}
	if recovered != 2 {
		t.Errorf("expected 2 recovered events, got %d", recovered)
	}

	// Verify both operations are SUCCEEDED
	op1U, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op1.ID)
	op2U, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op2.ID)
	if op1U.Status != "SUCCEEDED" || op2U.Status != "SUCCEEDED" {
		t.Errorf("operations must be SUCCEEDED after recovery worker, got op1=%s, op2=%s", op1U.Status, op2U.Status)
	}
}

func TestWebhook_OutOfOrder_ProcessingAfterSucceeded(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-OO-1", "ITEM-OO-1", "SB-OO1", "SI-OO1", false)
	op.Status = "SUCCEEDED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)

	// Delayed batch processing event arrives
	payload := map[string]interface{}{
		"id":         "WH-EVT-STALE-PROC",
		"event_type": paypal.EventPayoutsBatchProcessing,
		"resource": map[string]interface{}{
			"batch_header": map[string]interface{}{
				"payout_batch_id": "BATCH-OO-1",
				"batch_status":    "PROCESSING",
			},
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-oo1", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("CRITICAL INVARIANT VIOLATION: delayed PROCESSING downgraded SUCCEEDED operation to %s", opAfter.Status)
	}
}

func TestWebhook_OutOfOrder_UnclaimedAfterSucceeded(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-OO-2", "ITEM-OO-2", "SB-OO2", "SI-OO2", false)
	op.Status = "SUCCEEDED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)

	// Delayed unclaimed event arrives
	payload := map[string]interface{}{
		"id":         "WH-EVT-STALE-UNC",
		"event_type": paypal.EventPayoutsItemUnclaimed,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-OO-2",
			"transaction_status": "UNCLAIMED",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-oo2", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("CRITICAL INVARIANT VIOLATION: delayed UNCLAIMED downgraded SUCCEEDED operation to %s", opAfter.Status)
	}
}

func TestWebhook_OutOfOrder_FailedAfterSucceeded_PreservesFinancialState(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	pCase, op := env.createTestOperation(t, orgID, "BATCH-OO-3", "ITEM-OO-3", "SB-OO3", "SI-OO3", false)
	op.Status = "SUCCEEDED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)
	pCase.Status = "COMPLETED"
	_ = env.repo.UpdatePayoutCaseStatus(ctx, orgID, pCase.ID, "COMPLETED")

	// Conflicting failure arrives after operation already settled
	payload := map[string]interface{}{
		"id":         "WH-EVT-CONFLICT-FAIL",
		"event_type": paypal.EventPayoutsItemFailed,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-OO-3",
			"transaction_status": "FAILED",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-oo3", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	// Authoritative Financial State Rule: Must NEVER downgrade settled SUCCEEDED payout to RECONCILIATION_REQUIRED or FAILED!
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("CRITICAL VIOLATION: contradictory failure downgraded settled SUCCEEDED payout to %s", opAfter.Status)
	}
	caseAfter, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if caseAfter.Status != "COMPLETED" {
		t.Errorf("CRITICAL VIOLATION: case status must remain COMPLETED, got %s", caseAfter.Status)
	}
}

func TestWebhook_OutOfOrder_SucceededAfterCancelled_TransitionsToSucceeded(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	pCase, op := env.createTestOperation(t, orgID, "BATCH-OO-4", "ITEM-OO-4", "SB-OO4", "SI-OO4", false)
	op.Status = "CANCELLED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)
	pCase.Status = "CANCELLED"
	_ = env.repo.UpdatePayoutCaseStatus(ctx, orgID, pCase.ID, "CANCELLED")

	// Success arrives after cancellation! Provider actually executed the payout!
	payload := map[string]interface{}{
		"id":         "WH-EVT-CONFLICT-SUCC",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-OO-4",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-oo4", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	// Authoritative provider evidence confirms funds left the account.
	// Transitioning to SUCCEEDED truthfully reflects financial state and prevents duplicate resubmission!
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected cancelled item with provider success confirmation to transition to SUCCEEDED, got %s", opAfter.Status)
	}
	caseAfter, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if caseAfter.Status != "COMPLETED" {
		t.Errorf("expected case to transition to COMPLETED to prevent duplicate payouts, got %s", caseAfter.Status)
	}
}

func TestWebhook_OutOfOrder_SucceededAfterFailed_Recovery(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	pCase, op := env.createTestOperation(t, orgID, "BATCH-OO-5", "ITEM-OO-5", "SB-OO5", "SI-OO5", false)
	op.Status = "FAILED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)
	pCase.Status = "FAILED"
	_ = env.repo.UpdatePayoutCaseStatus(ctx, orgID, pCase.ID, "FAILED")

	// Provider confirms success!
	payload := map[string]interface{}{
		"id":         "WH-EVT-RECOVER-SUCC",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-OO-5",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-oo5", "2026-11-07T02:00:00Z", rawBody)

	_, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected operation to recover to SUCCEEDED, got %s", opAfter.Status)
	}
	caseAfter, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if caseAfter.Status != "COMPLETED" {
		t.Errorf("expected case to recover to COMPLETED, got %s", caseAfter.Status)
	}
}

func TestWebhook_OutOfOrder_ReturnedAfterSucceeded_PreservesSucceededStatus(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	pCase, op := env.createTestOperation(t, orgID, "BATCH-RET-1", "ITEM-RET-1", "SB-RET1", "SI-RET1", false)
	op.Status = "SUCCEEDED"
	_ = env.repo.UpdatePayoutOperation(ctx, op)
	pCase.Status = "COMPLETED"
	_ = env.repo.UpdatePayoutCaseStatus(ctx, orgID, pCase.ID, "COMPLETED")

	// Post-settlement return arrives
	payload := map[string]interface{}{
		"id":         "WH-EVT-RET-001",
		"event_type": paypal.EventPayoutsItemReturned,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-RET-1",
			"transaction_status": "RETURNED",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-ret1", "2026-11-07T02:00:00Z", rawBody)

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("webhook error: %v", err)
	}
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected event status PROCESSED, got %s", res.Event.ProcessingStatus)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	// Financial state remains SUCCEEDED (settlement history preserved)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("post-settlement return must preserve op status SUCCEEDED, got %s", opAfter.Status)
	}
	// Item status accurately tracks provider state
	if opAfter.PayPalItemStatus == nil || *opAfter.PayPalItemStatus != "RETURNED" {
		t.Errorf("expected provider item status RETURNED, got %v", opAfter.PayPalItemStatus)
	}
	caseAfter, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if caseAfter.Status != "COMPLETED" {
		t.Errorf("case status must remain COMPLETED, got %s", caseAfter.Status)
	}
}

func TestWebhook_UnsupportedItemDeniedEvent_SafelyIgnored(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	pCase, op := env.createTestOperation(t, orgID, "BATCH-CAT-1", "ITEM-CAT-1", "SB-CAT1", "SI-CAT1", false)

	// PAYMENT.PAYOUTS-ITEM.DENIED is unsupported per official PayPal Payouts event catalogue.
	// Only PAYMENT.PAYOUTSBATCH.DENIED exists in PayPal's API.
	payload := map[string]interface{}{
		"id":         "WH-EVT-UNSUPPORTED-ITEM-DENIED",
		"event_type": "PAYMENT.PAYOUTS-ITEM.DENIED",
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-CAT-1",
			"transaction_status": "DENIED",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-cat1", "2026-11-07T02:00:00Z", rawBody)

	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("unsupported event should be safely handled without error, got: %v", err)
	}
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected status PROCESSED, got %s", res.Event.ProcessingStatus)
	}

	// Invariant: No financial mutation must occur
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUBMITTED" {
		t.Errorf("unsupported event must NOT mutate operation status, got %s", opAfter.Status)
	}
	caseAfter, _ := env.repo.GetPayoutCaseByID(ctx, orgID, pCase.ID)
	if caseAfter.Status != "SUBMITTED" {
		t.Errorf("unsupported event must NOT mutate case status, got %s", caseAfter.Status)
	}

	// Verify catalogue helper
	if paypal.IsSupportedWebhookEventType("PAYMENT.PAYOUTS-ITEM.DENIED") {
		t.Errorf("PAYMENT.PAYOUTS-ITEM.DENIED must NOT be recognized as supported event")
	}
	if !paypal.IsSupportedWebhookEventType(paypal.EventPayoutsBatchDenied) {
		t.Errorf("PAYMENT.PAYOUTSBATCH.DENIED must be recognized as supported event")
	}
}

func TestWebhook_AtomicClaiming_ConcurrentExistingReceived(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-CONC-REC", "ITEM-CONC-REC", "SB-CR", "SI-CR", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-CONC-REC",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-CONC-REC",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-crec", "2026-11-07T02:00:00Z", rawBody)

	// Pre-seed event as RECEIVED (simulating prior delivery before crash)
	var rawSnapshot map[string]interface{}
	_ = json.Unmarshal(rawBody, &rawSnapshot)
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:     "WH-EVT-CONC-REC",
		EventType:         paypal.EventPayoutsItemSucceeded,
		ResourceType:      "payouts_item",
		TransmissionID:    headers.TransmissionID,
		WebhookID:         env.cfg.PayPalWebhookID,
		SignatureVerified: true,
		ProcessingStatus:  "RECEIVED",
		PayloadSnapshot:   rawSnapshot,
		CreatedAt:         time.Now().UTC().Add(-2 * time.Minute),
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	results := make([]*WebhookProcessResult, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("routine %d failed: %v", i, err)
		}
	}

	// Verify operation status transitioned to SUCCEEDED exactly once
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected operation SUCCEEDED, got %s", opAfter.Status)
	}
}

func TestWebhook_AtomicClaiming_ExpiredLeaseRecovery(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-EXP-1", "ITEM-EXP-1", "SB-EXP", "SI-EXP", false)

	payload := map[string]interface{}{
		"id":         "WH-EVT-EXP-LEASE",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-EXP-1",
			"transaction_status": "SUCCESS",
		},
	}
	rawBody, _ := json.Marshal(payload)
	headers, _ := env.sign(t, "t-exp", "2026-11-07T02:00:00Z", rawBody)

	// Simulate event claimed by worker that crashed 2 minutes ago (lease expired)
	token := "expired-worker-token"
	expiredLease := time.Now().UTC().Add(-1 * time.Minute)
	var rawSnapshot map[string]interface{}
	_ = json.Unmarshal(rawBody, &rawSnapshot)
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:      "WH-EVT-EXP-LEASE",
		EventType:          paypal.EventPayoutsItemSucceeded,
		ResourceType:       "payouts_item",
		TransmissionID:     headers.TransmissionID,
		WebhookID:          env.cfg.PayPalWebhookID,
		SignatureVerified:  true,
		ProcessingStatus:   "PROCESSING",
		ClaimToken:         &token,
		LeaseExpiresAt:     &expiredLease,
		ProcessingAttempts: 1,
		PayloadSnapshot:    rawSnapshot,
		CreatedAt:          time.Now().UTC().Add(-3 * time.Minute),
	})

	// New redelivery arrives: should detect expired lease, atomically reclaim, and finalize!
	res, err := env.service.ProcessPayPalWebhook(ctx, headers, rawBody)
	if err != nil {
		t.Fatalf("redelivery should recover expired lease, got: %v", err)
	}
	if res.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected status PROCESSED after lease recovery, got %s", res.Event.ProcessingStatus)
	}

	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected op SUCCEEDED after lease recovery, got %s", opAfter.Status)
	}
}

func TestWebhook_RetryBackoff_AndDeadLetter(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	// Create event with 4 failed attempts
	failedStatus := "FAILED"
	errMsg := "transient database failure"
	ev := &db.PayPalWebhookEvent{
		PayPalEventID:      "WH-EVT-RETRY-MAX",
		EventType:          paypal.EventPayoutsItemSucceeded,
		ResourceType:       "payouts_item",
		TransmissionID:     "t-max",
		WebhookID:          env.cfg.PayPalWebhookID,
		SignatureVerified:  true,
		ProcessingStatus:   failedStatus,
		ProcessingError:    &errMsg,
		ProcessingAttempts: 4,
		PayloadSnapshot:    map[string]interface{}{"id": "WH-EVT-RETRY-MAX"},
		CreatedAt:          time.Now().UTC().Add(-10 * time.Minute),
	}
	_ = env.repo.CreateWebhookEvent(ctx, ev)

	// Simulating 5th failure: should transition to DEAD_LETTER
	token := "attempt-5-token"
	lease := time.Now().UTC().Add(30 * time.Second)
	claimed, err := env.repo.ClaimWebhookEventForProcessing(ctx, ev.ID, token, lease)
	if err != nil {
		t.Fatalf("failed claiming event: %v", err)
	}
	if claimed.ProcessingAttempts != 5 {
		t.Errorf("expected attempts 5, got %d", claimed.ProcessingAttempts)
	}

	deadErr := "fatal error on attempt 5"
	finErr := env.repo.FinalizeWebhookEventProcessing(ctx, claimed.ID, token, "DEAD_LETTER", &deadErr, nil, nil, nil)
	if finErr != nil {
		t.Fatalf("failed finalizing to DEAD_LETTER: %v", finErr)
	}

	updated, _ := env.repo.GetWebhookEventByPayPalEventID(ctx, "WH-EVT-RETRY-MAX")
	if updated.ProcessingStatus != "DEAD_LETTER" {
		t.Errorf("expected DEAD_LETTER, got %s", updated.ProcessingStatus)
	}

	// Verify DEAD_LETTER cannot be claimed
	_, claimErr := env.repo.ClaimWebhookEventForProcessing(ctx, updated.ID, "token-6", lease)
	if claimErr == nil {
		t.Errorf("expected error claiming DEAD_LETTER event")
	}
}

func TestWebhookRecoveryWorker_Lifecycle(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orgID := "org-1"
	_, op := env.createTestOperation(t, orgID, "BATCH-LIFECYCLE-1", "ITEM-LIFECYCLE-1", "SB-LC", "SI-LC", false)

	// Seed stuck RECEIVED event
	payload := map[string]interface{}{
		"id":         "WH-EVT-LC-001",
		"event_type": paypal.EventPayoutsItemSucceeded,
		"resource": map[string]interface{}{
			"payout_item_id":     "ITEM-LIFECYCLE-1",
			"transaction_status": "SUCCESS",
		},
	}
	_ = env.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
		PayPalEventID:    "WH-EVT-LC-001",
		EventType:        paypal.EventPayoutsItemSucceeded,
		ProcessingStatus: "RECEIVED",
		PayloadSnapshot:  payload,
		CreatedAt:        time.Now().UTC().Add(-2 * time.Minute),
	})

	worker := env.service.NewWebhookRecoveryWorker(100*time.Millisecond, 10*time.Millisecond, 10)
	if worker.IsRunning() {
		t.Errorf("worker must not be running before Start")
	}

	// 1. Start exactly once
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("worker Start failed: %v", err)
	}
	if !worker.IsRunning() {
		t.Errorf("worker must be running after Start")
	}
	// Calling Start a second time must return error
	if err := worker.Start(ctx); err == nil {
		t.Errorf("second Start call must return error")
	}

	// Allow initial sweep to execute
	time.Sleep(200 * time.Millisecond)

	// Verify stuck event was recovered
	opAfter, _ := env.repo.GetPayoutOperationByID(ctx, orgID, op.ID)
	if opAfter.Status != "SUCCEEDED" {
		t.Errorf("expected worker to recover stuck event to SUCCEEDED, got %s", opAfter.Status)
	}

	// 2. Graceful stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	if err := worker.Stop(shutdownCtx); err != nil {
		t.Fatalf("worker Stop failed: %v", err)
	}
	if worker.IsRunning() {
		t.Errorf("worker must not be running after Stop")
	}
}

func TestWebhook_Mission040_7_DualContext_SimulatorAndRegistered(t *testing.T) {
	env := setupWebhookTestEnv(t)
	ctx := context.Background()

	const (
		simulatorWebhookID  = "WEBHOOK_ID"
		registeredWebhookID = "71R95625WE243291S"
	)

	// Context A: Simulator Service Configuration
	cfgSim := *env.cfg
	cfgSim.PayPalWebhookID = simulatorWebhookID
	serviceSim := NewService(&cfgSim, env.repo, nil, nil)
	serviceSim.SetWebhookVerifier(env.verifier)

	// Context B: Registered Webhook Service Configuration
	cfgReg := *env.cfg
	cfgReg.PayPalWebhookID = registeredWebhookID
	serviceReg := NewService(&cfgReg, env.repo, nil, nil)
	serviceReg.SetWebhookVerifier(env.verifier)

	// Create matching local operations so the verified webhooks reconcile to PROCESSED
	orgID := "org-m0407"
	simBatchID := "SIM-BATCH-001"
	simItemID := "SIM-ITEM-001"
	_, opSim := env.createTestOperation(t, orgID, simBatchID, simItemID, "SB-SIM-001", "SI-SIM-001", false)

	regBatchID := "REG-BATCH-001"
	regItemID := "REG-ITEM-001"
	_, opReg := env.createTestOperation(t, orgID, regBatchID, regItemID, "SB-REG-001", "SI-REG-001", false)

	// Setup payload for batch processing event (Simulator)
	payload := map[string]interface{}{
		"id":            "WH-M0407-SIM-001",
		"event_version": "1.0",
		"create_time":   "2026-11-09T08:00:00Z",
		"resource_type": "payouts_item",
		"event_type":    paypal.EventPayoutsItemSucceeded,
		"summary":       "Payouts item succeeded simulated",
		"resource": map[string]interface{}{
			"payout_item_id":     simItemID,
			"payout_batch_id":    simBatchID,
			"transaction_id":     "TX-SIM-001",
			"transaction_status": "SUCCESS",
			"sender_item_id":     "SI-SIM-001",
		},
	}
	rawBody, _ := json.Marshal(payload)
	transID := "trans-sim-001"
	transTime := "2026-11-09T08:00:01Z"

	// 1. Sign using Simulator Webhook ID ("WEBHOOK_ID")
	sigSim, err := paypal.SignWebhookPayload(env.privKey, transID, transTime, simulatorWebhookID, rawBody)
	if err != nil {
		t.Fatalf("failed signing simulator payload: %v", err)
	}
	headersSim := paypal.WebhookHeaders{
		TransmissionID:   transID,
		TransmissionTime: transTime,
		CertURL:          env.certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sigSim,
	}

	// 1a. Simulator event accepted by Simulator Service Context
	resSim, err := serviceSim.ProcessPayPalWebhook(ctx, headersSim, rawBody)
	if err != nil {
		t.Fatalf("expected simulator event to be accepted by simulator context, got: %v", err)
	}
	if resSim.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected PROCESSED, got %s", resSim.Event.ProcessingStatus)
	}
	if resSim.MatchedOperation == nil || resSim.MatchedOperation.ID != opSim.ID {
		t.Errorf("expected matched operation %s, got %v", opSim.ID, resSim.MatchedOperation)
	}

	// 1b. Fail-closed: Simulator signature REJECTED by Registered Service Context
	// (Replicates and explains Mission 040.3/040.4 forensic finding)
	_, errRegReject := serviceReg.ProcessPayPalWebhook(ctx, headersSim, rawBody)
	if errRegReject == nil {
		t.Fatalf("expected registered context to reject simulator signature, got nil")
	}

	// 2. Sign using Registered Webhook ID ("71R95625WE243291S")
	payloadReg := map[string]interface{}{
		"id":            "WH-M0407-REG-001",
		"event_version": "1.0",
		"create_time":   "2026-11-09T08:01:00Z",
		"resource_type": "payouts_item",
		"event_type":    paypal.EventPayoutsItemSucceeded,
		"summary":       "Genuine registered webhook event",
		"resource": map[string]interface{}{
			"payout_item_id":     regItemID,
			"payout_batch_id":    regBatchID,
			"transaction_id":     "TX-REG-001",
			"transaction_status": "SUCCESS",
			"sender_item_id":     "SI-REG-001",
		},
	}
	rawBodyReg, _ := json.Marshal(payloadReg)
	transIDReg := "trans-reg-001"
	transTimeReg := "2026-11-09T08:01:01Z"

	sigReg, err := paypal.SignWebhookPayload(env.privKey, transIDReg, transTimeReg, registeredWebhookID, rawBodyReg)
	if err != nil {
		t.Fatalf("failed signing registered payload: %v", err)
	}
	headersReg := paypal.WebhookHeaders{
		TransmissionID:   transIDReg,
		TransmissionTime: transTimeReg,
		CertURL:          env.certURL,
		AuthAlgo:         "SHA256withRSA",
		TransmissionSig:  sigReg,
	}

	// 2a. Registered event accepted by Registered Service Context
	resReg, err := serviceReg.ProcessPayPalWebhook(ctx, headersReg, rawBodyReg)
	if err != nil {
		t.Fatalf("expected registered event to be accepted by registered context, got: %v", err)
	}
	if resReg.Event.ProcessingStatus != "PROCESSED" {
		t.Errorf("expected PROCESSED, got %s", resReg.Event.ProcessingStatus)
	}
	if resReg.MatchedOperation == nil || resReg.MatchedOperation.ID != opReg.ID {
		t.Errorf("expected matched operation %s, got %v", opReg.ID, resReg.MatchedOperation)
	}

	// 2b. Fail-closed: Registered signature REJECTED by Simulator Service Context
	_, errSimReject := serviceSim.ProcessPayPalWebhook(ctx, headersReg, rawBodyReg)
	if errSimReject == nil {
		t.Fatalf("expected simulator context to reject registered signature, got nil")
	}

	// 3. Tampering resistance in both contexts
	// 3a. Modified payload
	tamperedBody := append(rawBodyReg, byte(' '))
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersReg, tamperedBody); err == nil {
		t.Errorf("expected modified payload rejection")
	}

	// 3b. Modified transmission ID
	headersTamperedID := headersReg
	headersTamperedID.TransmissionID = "trans-tampered"
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersTamperedID, rawBodyReg); err == nil {
		t.Errorf("expected modified transmission ID rejection")
	}

	// 3c. Modified timestamp
	headersTamperedTime := headersReg
	headersTamperedTime.TransmissionTime = "2026-11-09T08:01:02Z"
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersTamperedTime, rawBodyReg); err == nil {
		t.Errorf("expected modified transmission time rejection")
	}

	// 3d. Modified signature bytes
	headersTamperedSig := headersReg
	headersTamperedSig.TransmissionSig = sigSim // wrong sig
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersTamperedSig, rawBodyReg); err == nil {
		t.Errorf("expected wrong signature rejection")
	}

	// 4. Replay Protection & Idempotency
	resReplay, err := serviceReg.ProcessPayPalWebhook(ctx, headersReg, rawBodyReg)
	if err != nil {
		t.Fatalf("expected replay to be handled gracefully, got error: %v", err)
	}
	if !resReplay.IsDuplicate {
		t.Errorf("expected IsDuplicate = true on replayed event")
	}

	// 5. SSRF / Invalid Cert URL rejection
	headersSSRF := headersReg
	headersSSRF.CertURL = "https://evil.com/cert.pem"
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersSSRF, rawBodyReg); err == nil {
		t.Errorf("expected SSRF non-paypal domain rejection")
	}

	headersHTTP := headersReg
	headersHTTP.CertURL = "http://api.sandbox.paypal.com/cert.pem"
	if _, err := serviceReg.ProcessPayPalWebhook(ctx, headersHTTP, rawBodyReg); err == nil {
		t.Errorf("expected non-https scheme rejection")
	}
}
