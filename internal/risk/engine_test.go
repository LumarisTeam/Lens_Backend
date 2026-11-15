package risk

import (
	"strings"
	"testing"
)

func TestContentHashIgnoresWhitespaceCaseAndPunctuation(t *testing.T) {
	a := ContentHash("  Login   CRASH!!! ")
	b := ContentHash("login crash")
	if a != b {
		t.Fatalf("normalized hashes differ: %s != %s", a, b)
	}
}

func TestSimilarityDetectsMinorVariation(t *testing.T) {
	score := Similarity(
		"课表页面切换周次后闪退，重新打开仍然无法恢复。",
		"课表页面切换周次后会闪退，重新打开仍然无法恢复",
	)
	if score < 0.8 {
		t.Fatalf("similarity = %.2f, want >= 0.8", score)
	}
	if Similarity("登录按钮无法点击", "图片上传后空白") >= 0.5 {
		t.Fatal("unrelated texts unexpectedly have high similarity")
	}
}

func TestEvaluateBurstAndDuplicateReachChallenge(t *testing.T) {
	got := Evaluate(Signals{
		IPFeedback24h:     20,
		IPFeedback10m:     6,
		ExactDuplicate24h: 1,
		ImageCount:        2,
	}, Policy{
		ReviewThreshold:     40,
		ChallengeThreshold:  55,
		BlockThreshold:      80,
		SimilarityThreshold: 0.85,
	})
	if got.Score < 55 {
		t.Fatalf("score = %d, want challenge range", got.Score)
	}
	if !got.RequiresCaptcha {
		t.Fatalf("RequiresCaptcha = false, reasons: %+v", got.Reasons)
	}
}

func TestEvaluateCaptchaReducesRisk(t *testing.T) {
	signals := Signals{
		IPFeedback24h:     20,
		IPFeedback10m:     6,
		ExactDuplicate24h: 1,
	}
	policy := Policy{
		ReviewThreshold:     40,
		ChallengeThreshold:  55,
		BlockThreshold:      80,
		SimilarityThreshold: 0.85,
	}
	before := Evaluate(signals, policy)
	signals.CaptchaPassed = true
	after := Evaluate(signals, policy)
	if after.Score >= before.Score {
		t.Fatalf("captcha did not reduce score: before=%d after=%d", before.Score, after.Score)
	}
	if after.RequiresCaptcha {
		t.Fatalf("captcha should not be requested twice: %+v", after.Reasons)
	}
}

func TestEvaluateIncludesIPAttachmentVolume(t *testing.T) {
	got := Evaluate(Signals{IPImages24h: 50}, Policy{
		ReviewThreshold:     40,
		ChallengeThreshold:  55,
		BlockThreshold:      80,
		SimilarityThreshold: 0.85,
	})
	if got.Score == 0 {
		t.Fatalf("attachment volume not scored: %+v", got.Reasons)
	}
}

func TestNormalizeContentDoesNotGrowUnbounded(t *testing.T) {
	got := NormalizeContent(strings.Repeat("测试 Feedback!!! ", 1000))
	if strings.ContainsAny(got, " !") || strings.Contains(got, "FEEDBACK") {
		t.Fatalf("normalization did not strip case/space/punctuation: %q", got[:40])
	}
}
