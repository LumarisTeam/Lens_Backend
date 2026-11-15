// Package risk 提供可在本进程运行的确定性反馈风险分析规则。
// 规则只使用频率、重复度、附件和历史处置信号，不依赖外部模型。
package risk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"lens-backend/internal/model"
)

// Policy 定义评分阈值。
type Policy struct {
	ReviewThreshold     int
	ChallengeThreshold  int
	BlockThreshold      int
	SimilarityThreshold float64
}

// Signals 是评分所需的聚合特征。
type Signals struct {
	IPFeedback24h     int64
	ClientFeedback24h int64
	IPFeedback10m     int64
	ClientFeedback10m int64
	ExactDuplicate24h int64
	SimilarMax        float64
	SimilarCount      int
	ImageCount        int
	IPImages24h       int64
	IPBlockedEvents7d int64
	CaptchaPassed     bool
}

// Reason 是一条可审计的评分原因。
type Reason struct {
	Code   string `json:"code"`
	Score  int    `json:"score"`
	Detail string `json:"detail"`
}

// Assessment 是风险分析结果。
type Assessment struct {
	Score           int
	Reasons         []Reason
	ReviewStatus    string
	RequiresCaptcha bool
	Action          string
}

// Evaluate 根据规则生成 0 到 100 的风险分。
func Evaluate(signals Signals, policy Policy) Assessment {
	reasons := make([]Reason, 0, 8)
	add := func(code string, score int, detail string) {
		if score == 0 {
			return
		}
		reasons = append(reasons, Reason{Code: code, Score: score, Detail: detail})
	}

	switch {
	case signals.IPFeedback24h >= 30:
		add("ip_daily_volume", 30, fmt.Sprintf("同一 IP 24 小时提交 %d 条", signals.IPFeedback24h))
	case signals.IPFeedback24h >= 15:
		add("ip_daily_volume", 15, fmt.Sprintf("同一 IP 24 小时提交 %d 条", signals.IPFeedback24h))
	}
	switch {
	case signals.ClientFeedback24h >= 50:
		add("client_daily_volume", 20, fmt.Sprintf("同一 Client ID 24 小时提交 %d 条", signals.ClientFeedback24h))
	case signals.ClientFeedback24h >= 20:
		add("client_daily_volume", 10, fmt.Sprintf("同一 Client ID 24 小时提交 %d 条", signals.ClientFeedback24h))
	}
	switch {
	case signals.IPFeedback10m >= 10:
		add("ip_burst", 35, fmt.Sprintf("同一 IP 10 分钟提交 %d 条", signals.IPFeedback10m))
	case signals.IPFeedback10m >= 5:
		add("ip_burst", 20, fmt.Sprintf("同一 IP 10 分钟提交 %d 条", signals.IPFeedback10m))
	}
	if signals.ClientFeedback10m >= 5 {
		add("client_burst", 15, fmt.Sprintf("同一 Client ID 10 分钟提交 %d 条", signals.ClientFeedback10m))
	}
	switch {
	case signals.ExactDuplicate24h >= 3:
		add("exact_duplicate", 50, fmt.Sprintf("24 小时内相同文本已出现 %d 次", signals.ExactDuplicate24h))
	case signals.ExactDuplicate24h >= 1:
		add("exact_duplicate", 35, "24 小时内存在相同文本")
	}
	switch {
	case signals.SimilarMax >= 0.95:
		add("near_duplicate", 40, fmt.Sprintf("最高文本相似度 %.2f", signals.SimilarMax))
	case signals.SimilarMax >= policy.SimilarityThreshold:
		add("near_duplicate", 20, fmt.Sprintf("最高文本相似度 %.2f", signals.SimilarMax))
	}
	if signals.SimilarCount >= 3 {
		add("similar_cluster", 15, fmt.Sprintf("24 小时内发现 %d 条相似反馈", signals.SimilarCount))
	}
	if signals.ImageCount >= 6 {
		add("attachment_volume", 10, fmt.Sprintf("单条反馈包含 %d 张图片", signals.ImageCount))
	}
	switch {
	case signals.IPImages24h >= 50:
		add("ip_attachment_volume", 25, fmt.Sprintf("同一 IP 24 小时创建 %d 个上传凭证", signals.IPImages24h))
	case signals.IPImages24h >= 20:
		add("ip_attachment_volume", 10, fmt.Sprintf("同一 IP 24 小时创建 %d 个上传凭证", signals.IPImages24h))
	}
	switch {
	case signals.IPBlockedEvents7d >= 3:
		add("ip_reputation", 30, fmt.Sprintf("同一 IP 7 天内被拦截 %d 次", signals.IPBlockedEvents7d))
	case signals.IPBlockedEvents7d >= 1:
		add("ip_reputation", 15, fmt.Sprintf("同一 IP 7 天内被拦截 %d 次", signals.IPBlockedEvents7d))
	}
	if signals.CaptchaPassed {
		add("captcha_passed", -30, "人机验证已通过，风险分下调")
	}

	score := 0
	for _, reason := range reasons {
		score += reason.Score
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	out := Assessment{Score: score, Reasons: reasons}
	if score >= policy.BlockThreshold {
		out.ReviewStatus = model.FeedbackReviewBlocked
		out.Action = model.RiskActionBlocked
	} else if score >= policy.ReviewThreshold {
		out.ReviewStatus = model.FeedbackReviewReview
		out.Action = model.RiskActionReview
	} else {
		out.ReviewStatus = model.FeedbackReviewNormal
		out.Action = model.RiskActionAllow
	}
	out.RequiresCaptcha = score >= policy.ChallengeThreshold
	if out.RequiresCaptcha && score < policy.BlockThreshold {
		out.Action = model.RiskActionChallenge
	}
	return out
}

// ContentHash 返回规范化文本的 SHA-256，用于跨实例精确去重。
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(NormalizeContent(content)))
	return hex.EncodeToString(sum[:])
}

// NormalizeContent 去除大小写、空白、标点和符号差异，降低简单变形绕过。
func NormalizeContent(content string) string {
	content = norm.NFKC.String(content)
	var b strings.Builder
	for _, r := range strings.ToLower(content) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Similarity 返回两个文本基于字符三元组的 Dice 相似度。
func Similarity(a, b string) float64 {
	ga := ngrams(NormalizeContent(a), 3)
	gb := ngrams(NormalizeContent(b), 3)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	inter := 0
	for gram := range ga {
		if _, ok := gb[gram]; ok {
			inter++
		}
	}
	total := len(ga) + len(gb)
	if total == 0 {
		return 0
	}
	return 2 * float64(inter) / float64(total)
}

func ngrams(s string, n int) map[string]struct{} {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) < n {
		n = len(runes)
	}
	out := make(map[string]struct{}, len(runes)-n+1)
	for i := 0; i+n <= len(runes); i++ {
		out[string(runes[i:i+n])] = struct{}{}
	}
	return out
}
