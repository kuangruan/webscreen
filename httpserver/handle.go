package httpserver

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func HandleStatic(c *gin.Context) {
	http.ServeFile(c.Writer, c.Request, "./public"+c.Request.URL.Path)
}
