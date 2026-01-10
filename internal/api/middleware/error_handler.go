package middleware

import (
	"log"
	"net/http"

	"github.com/cyanstack/cyanstack/internal/api/dto"
	"github.com/gin-gonic/gin"
)

// ErrorHandler returns a middleware that recovers from panics and returns JSON errors
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic recovered: %v", err)

				c.JSON(http.StatusInternalServerError, dto.ErrorResponse{
					Error: "Internal server error",
					Code:  dto.ErrCodeInternal,
				})
				c.Abort()
			}
		}()

		c.Next()

		// Handle any errors that were set during request processing
		if len(c.Errors) > 0 {
			err := c.Errors.Last()
			log.Printf("Request error: %v", err.Err)

			// If status hasn't been set, default to 500
			if c.Writer.Status() == http.StatusOK {
				c.JSON(http.StatusInternalServerError, dto.ErrorResponse{
					Error: err.Error(),
					Code:  dto.ErrCodeInternal,
				})
			}
		}
	}
}
