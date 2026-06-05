package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/models"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/services"
	"github.com/znsio/specmatic-order-bff-go/pkg/utils"
)

type OrderController struct {
	BackendService *services.BackendService
}

func (oc *OrderController) CreateOrder(c *gin.Context) {
	var newOrder models.OrderRequest

	if c.ContentType() != "application/json" {
        c.JSON(http.StatusUnsupportedMediaType, gin.H{
            "error": "Unsupported Media Type",
            "message": "Content-Type must be application/json",
        })
        return
    }

	// Bind JSON to NewProduct Model (struct) and validate the request based on model rules.
	if err := c.ShouldBindJSON(&newOrder); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	// Call service to create the product
	orderID, err := oc.BackendService.CreateOrder(newOrder)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id": orderID,
	})
}
