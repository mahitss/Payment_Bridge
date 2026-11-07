package paypal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Supported PayPal Payout Webhook Event Types
const (
	// Batch Events
	EventPayoutsBatchDenied     = "PAYMENT.PAYOUTSBATCH.DENIED"
	EventPayoutsBatchProcessing = "PAYMENT.PAYOUTSBATCH.PROCESSING"
	EventPayoutsBatchSuccess    = "PAYMENT.PAYOUTSBATCH.SUCCESS"

	// Item Events
	EventPayoutsItemBlocked   = "PAYMENT.PAYOUTS-ITEM.BLOCKED"
	EventPayoutsItemCanceled  = "PAYMENT.PAYOUTS-ITEM.CANCELED"
	EventPayoutsItemFailed    = "PAYMENT.PAYOUTS-ITEM.FAILED"
	EventPayoutsItemHeld      = "PAYMENT.PAYOUTS-ITEM.HELD"
	EventPayoutsItemRefunded  = "PAYMENT.PAYOUTS-ITEM.REFUNDED"
	EventPayoutsItemReturned  = "PAYMENT.PAYOUTS-ITEM.RETURNED"
	EventPayoutsItemSucceeded = "PAYMENT.PAYOUTS-ITEM.SUCCEEDED"
	EventPayoutsItemUnclaimed = "PAYMENT.PAYOUTS-ITEM.UNCLAIMED"
)

// WebhookEvent represents an incoming PayPal webhook notification envelope.
type WebhookEvent struct {
	ID           string            `json:"id"`
	EventVersion string            `json:"event_version"`
	CreateTime   time.Time         `json:"create_time"`
	ResourceType string            `json:"resource_type"`
	EventType    string            `json:"event_type"`
	Summary      string            `json:"summary"`
	Resource     json.RawMessage   `json:"resource"`
	Links        []LinkDescription `json:"links,omitempty"`
}

// WebhookPayoutBatchResource represents the resource object in a batch event.
type WebhookPayoutBatchResource struct {
	BatchHeader       *PayoutBatchHeader `json:"batch_header,omitempty"`
	PayoutBatchID     string             `json:"payout_batch_id,omitempty"`
	BatchStatus       string             `json:"batch_status,omitempty"`
	TimeCreated       string             `json:"time_created,omitempty"`
	TimeCompleted     string             `json:"time_completed,omitempty"`
	SenderBatchHeader *SenderBatchHeader `json:"sender_batch_header,omitempty"`
	Amount            *CurrencyAmount    `json:"amount,omitempty"`
	Fees              *CurrencyAmount    `json:"fees,omitempty"`
	Items             []PayoutItem       `json:"items,omitempty"`
}

// ParsePayoutBatch extracts and normalizes the PayoutBatchResponse from the webhook resource.
func (e *WebhookEvent) ParsePayoutBatch() (*PayoutBatchResponse, error) {
	if len(e.Resource) == 0 {
		return nil, errors.New("empty webhook event resource")
	}

	var batchRes WebhookPayoutBatchResource
	if err := json.Unmarshal(e.Resource, &batchRes); err != nil {
		return nil, err
	}

	resp := &PayoutBatchResponse{}

	if batchRes.BatchHeader != nil {
		resp.BatchHeader = *batchRes.BatchHeader
		resp.Items = batchRes.Items
	} else {
		resp.BatchHeader = PayoutBatchHeader{
			PayoutBatchID: batchRes.PayoutBatchID,
			BatchStatus:   batchRes.BatchStatus,
			TimeCreated:   batchRes.TimeCreated,
			TimeCompleted: batchRes.TimeCompleted,
			Amount:        batchRes.Amount,
			Fees:          batchRes.Fees,
		}
		if batchRes.SenderBatchHeader != nil {
			resp.BatchHeader.SenderBatchID = batchRes.SenderBatchHeader.SenderBatchID
		}
		resp.Items = batchRes.Items
	}

	resp.Normalize()
	return resp, nil
}

// WebhookPayoutItemResource represents the resource object in an item event.
type WebhookPayoutItemResource struct {
	PayoutItemID      string             `json:"payout_item_id"`
	TransactionID     string             `json:"transaction_id,omitempty"`
	TransactionStatus string             `json:"transaction_status"`
	PayoutBatchID     string             `json:"payout_batch_id"`
	PayoutItem        *PayoutItemDetail  `json:"payout_item,omitempty"`
	SenderItemID      string             `json:"sender_item_id,omitempty"`
	TimeProcessed     string             `json:"time_processed,omitempty"`
	Errors            json.RawMessage    `json:"errors,omitempty"`
	Links             []LinkDescription  `json:"links,omitempty"`
}

// ParsePayoutItem extracts and normalizes the PayoutItem and batch ID from the webhook resource.
func (e *WebhookEvent) ParsePayoutItem() (*PayoutItem, string, error) {
	if len(e.Resource) == 0 {
		return nil, "", errors.New("empty webhook event resource")
	}

	var itemRes WebhookPayoutItemResource
	if err := json.Unmarshal(e.Resource, &itemRes); err != nil {
		return nil, "", err
	}

	item := &PayoutItem{
		PayoutItemID:      itemRes.PayoutItemID,
		TransactionID:     itemRes.TransactionID,
		TransactionStatus: itemRes.TransactionStatus,
		SenderItemID:      itemRes.SenderItemID,
		PayoutItem:        itemRes.PayoutItem,
		TimeProcessed:     itemRes.TimeProcessed,
		Errors:            itemRes.Errors,
		Links:             itemRes.Links,
	}

	item.Normalize()
	return item, itemRes.PayoutBatchID, nil
}

// IsBatchEvent returns true if the event represents a batch-level status update.
func IsBatchEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "PAYMENT.PAYOUTSBATCH.")
}

// IsItemEvent returns true if the event represents an item-level status update.
func IsItemEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "PAYMENT.PAYOUTS-ITEM.")
}

// ValidateHATEOASURL validates that a HATEOAS link targets an official PayPal HTTPS endpoint without traversal.
func ValidateHATEOASURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return errors.New("hateoas URL is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("malformed hateoas URL: %w", err)
	}
	if u.Scheme != "https" {
		return errors.New("hateoas URL must use https scheme")
	}
	if u.User != nil {
		return errors.New("hateoas URL contains userinfo")
	}
	host := strings.ToLower(u.Hostname())
	if host != "api.paypal.com" && host != "api.sandbox.paypal.com" && host != "api-m.paypal.com" && host != "api-m.sandbox.paypal.com" && !strings.HasSuffix(host, ".paypal.com") {
		return fmt.Errorf("hateoas host %q is not a permitted PayPal host", host)
	}
	if port := u.Port(); port != "" && port != "443" {
		return fmt.Errorf("hateoas non-standard port %q rejected", port)
	}
	if strings.Contains(u.Path, "..") {
		return errors.New("hateoas path traversal rejected")
	}
	return nil
}
