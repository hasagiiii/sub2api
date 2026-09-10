package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageStatusFromTaskMapsTerminalFailureToFailed(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{name: "pending", status: service.AsyncMediaStatusPending, want: fal.StatusInQueue},
		{name: "running", status: service.AsyncMediaStatusRunning, want: fal.StatusInProgress},
		{name: "succeeded", status: service.AsyncMediaStatusSucceeded, want: fal.StatusCompleted},
		{name: "failed", status: service.AsyncMediaStatusFailed, want: fal.StatusFailed},
		{name: "refunded", status: service.AsyncMediaStatusRefunded, want: fal.StatusFailed},
		{name: "expired", status: service.AsyncMediaStatusExpired, want: fal.StatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imageStatusFromTask(&service.AsyncMediaTask{Status: tt.status})
			if got != tt.want {
				t.Fatalf("imageStatusFromTask(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestImageGatewayErrorResponseIncludesProjectErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	h := &ImageGatewayHandler{}
	h.jsonErrorWithCode(c, http.StatusBadRequest, "api_error", "INVALID_IMAGE_LAYER_DECOMPOSITION", "The image content could not be processed for layer decomposition.")

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{"error":{"type":"api_error","code":"INVALID_IMAGE_LAYER_DECOMPOSITION","message":"The image content could not be processed for layer decomposition."}}`, recorder.Body.String())
}
