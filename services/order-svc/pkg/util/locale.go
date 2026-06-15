package util

import "github.com/gin-gonic/gin"

const (
	ViLanguage      = "vi"
	EnLanguage      = "en"
	DefaultLanguage = ViLanguage
)

// GetLanguage returns the language of the request
func GetLanguage(c *gin.Context) string {
	lang := c.GetHeader("Lang")
	if lang == EnLanguage {
		return EnLanguage
	}

	return DefaultLanguage
}
