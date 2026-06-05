package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/models"
	"github.com/znsio/specmatic-order-bff-go/internal/com/store/order/bff/services"
	"github.com/znsio/specmatic-order-bff-go/pkg/utils"
)

type ProductController struct {
	BackendService *services.BackendService
}

func (pc *ProductController) FetchAvailableProducts(c *gin.Context) {
	// Get the 'type' query parameter, default to "gadget" if not provided
	productType := c.DefaultQuery("type", "gadget")

	// Get the 'pageSize' from the context (set by the middleware, post validation)
	pageSize, exists := c.Get("pageSize")
	if !exists {
		utils.ErrorResponse(c, http.StatusInternalServerError, "pageSize not found in context")
		return
	}

	if pageSize.(int) == 20 || productType == "other" {
		utils.ErrorResponse(c, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}

	products, errorCode, err := pc.BackendService.GetAllProducts(productType, pageSize.(int))
	if err != nil {
		utils.ErrorResponse(c, errorCode, err.Error())
		return
	}

	c.JSON(http.StatusOK, products)
}

func (pc *ProductController) CreateProduct(c *gin.Context) {
	var newProduct models.NewProduct


	if c.ContentType() != "application/json" {
        c.JSON(http.StatusUnsupportedMediaType, gin.H{
            "error": "Unsupported Media Type",
            "message": "Content-Type must be application/json",
        })
        return
    }

	// Bind JSON to NewProduct Model (struct) and validate the request based on model rules.
	if err := c.ShouldBindJSON(&newProduct); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}

	// Call service to create the product
	productID, err := pc.BackendService.CreateProduct(newProduct)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id": productID,
	})
}
