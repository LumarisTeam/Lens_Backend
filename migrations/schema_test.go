package migrations

import (
	"strings"
	"testing"
)

func TestUpgradeColumnsPrecedeNewIndexes(t *testing.T) {
	clientIPAlter := strings.Index(Schema, "ALTER TABLE feedback ADD COLUMN IF NOT EXISTS client_ip")
	clientIPIndex := strings.Index(Schema, "idx_feedback_client_ip_time")
	if clientIPAlter < 0 || clientIPIndex < 0 || clientIPAlter > clientIPIndex {
		t.Fatalf("feedback.client_ip must be added before its index")
	}

	centerAlter := strings.Index(Schema, "ALTER TABLE feedback ADD COLUMN IF NOT EXISTS center_id")
	centerIndex := strings.Index(Schema, "idx_feedback_center_time")
	if centerAlter < 0 || centerIndex < 0 || centerAlter > centerIndex {
		t.Fatalf("feedback.center_id must be added before its index")
	}

	attachmentAlter := strings.Index(Schema, "ALTER TABLE feedback_attachment ADD COLUMN IF NOT EXISTS client_ip")
	attachmentIndex := strings.Index(Schema, "idx_attachment_client_ip")
	if attachmentAlter < 0 || attachmentIndex < 0 || attachmentAlter > attachmentIndex {
		t.Fatalf("feedback_attachment.client_ip must be added before its index")
	}
}
