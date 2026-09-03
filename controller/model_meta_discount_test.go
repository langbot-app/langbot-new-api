package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func performModelMetaUpdate(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/models/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	UpdateModelMeta(ctx)
	return recorder
}

func TestUpdateModelMetaPreservesDiscountWhenOmitted(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	original := model.Model{ModelName: "discount-preserve-model", Description: "before", Discount: 80}
	require.NoError(t, db.Create(&original).Error)

	response := performModelMetaUpdate(t, `{"id":`+fmt.Sprintf("%d", original.Id)+`,"model_name":"discount-preserve-model","description":"after"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var updated model.Model
	require.NoError(t, db.First(&updated, original.Id).Error)
	require.Equal(t, float64(80), updated.Discount)
}

func TestUpdateModelMetaClearsDiscountWhenExplicitlyZero(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	original := model.Model{ModelName: "discount-clear-model", Discount: 80}
	require.NoError(t, db.Create(&original).Error)

	response := performModelMetaUpdate(t, `{"id":`+fmt.Sprintf("%d", original.Id)+`,"model_name":"discount-clear-model","discount":0}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var updated model.Model
	require.NoError(t, db.First(&updated, original.Id).Error)
	require.Zero(t, updated.Discount)
}
