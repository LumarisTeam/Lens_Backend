package service

import (
	"testing"
	"time"

	"lens-backend/internal/model"
)

func TestValidateSNConfig(t *testing.T) {
	mode, sns, err := validateSNConfig(
		model.FeedbackCenterSNModeWhitelist,
		[]string{" SN001 ", "SN001", "SN002"},
	)
	if err != nil {
		t.Fatalf("validateSNConfig() error = %v", err)
	}
	if mode != model.FeedbackCenterSNModeWhitelist {
		t.Fatalf("mode = %q, want whitelist", mode)
	}
	if len(sns) != 2 || sns[0].SN != "SN001" || sns[1].SN != "SN002" {
		t.Fatalf("sns = %+v, want deduplicated SN001/SN002", sns)
	}

	if _, _, err := validateSNConfig(model.FeedbackCenterSNModeWhitelist, nil); err == nil {
		t.Fatal("whitelist mode accepted empty snList")
	}
	if _, _, err := validateSNConfig("unknown", []string{"SN001"}); err == nil {
		t.Fatal("unknown snMode accepted")
	}
}

func TestValidateTimestampWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if err := validateTimestamp(now.Unix()-300, 300, now); err != nil {
		t.Fatalf("boundary timestamp rejected: %v", err)
	}
	if err := validateTimestamp(now.Unix()-301, 300, now); err == nil {
		t.Fatal("expired timestamp accepted")
	}
	if err := validateTimestamp(now.Unix()+301, 300, now); err == nil {
		t.Fatal("future timestamp accepted")
	}
}

func TestVerifyAndConsumeRequiresAllHeaders(t *testing.T) {
	svc := &FeedbackCenterService{}
	_, err := svc.VerifyAndConsume(t.Context(), model.FeedbackCodeHeaders{
		CenterID: "fc_test",
	})
	if err == nil {
		t.Fatal("VerifyAndConsume() accepted incomplete headers")
	}
}
