package middleware

import (
	"github.com/gin-gonic/gin"

	"lens-backend/internal/handler/response"
	"lens-backend/internal/model"
	"lens-backend/internal/service"
)

const (
	FeedbackCenterIDKey  = "feedback_center_id"
	FeedbackAppIDKey     = "feedback_app_id"
	FeedbackSNKey        = "feedback_sn"
	FeedbackTimestampKey = "feedback_timestamp"
)

// FeedbackCodeAuth 校验反馈提交接口的反馈中心校验头。
func FeedbackCodeAuth(center *service.FeedbackCenterService) gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := model.FeedbackCodeHeaders{
			ClientID:  c.GetString(ClientIDKey),
			CenterID:  c.GetHeader("X-Feedback-Center-Id"),
			Timestamp: c.GetHeader("X-Timestamp"),
			SN:        c.GetHeader("X-SN"),
			Nonce:     c.GetHeader("X-Nonce"),
			Code:      c.GetHeader("X-Code"),
		}
		centerMeta, err := center.VerifyAndConsume(c.Request.Context(), headers)
		if err != nil {
			response.WriteError(c, err)
			c.Abort()
			return
		}
		c.Set(FeedbackCenterIDKey, headers.CenterID)
		c.Set(FeedbackAppIDKey, centerMeta.AppID)
		c.Set(FeedbackSNKey, headers.SN)
		c.Set(FeedbackTimestampKey, headers.Timestamp)
		c.Next()
	}
}
