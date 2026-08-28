package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/fal"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	publicImageSubmitFailure = "Failed to submit image task. Please try again later."
	publicImageFailure       = "Image generation failed. Please try again later."
	publicImageAccountError  = "No available image account"
	publicVideoSubmitFailure = "Failed to submit video task. Please try again later."
	publicVideoFailure       = "Video generation failed. Please try again later."
)

type asyncImageResultResponse struct {
	Status     string      `json:"status"`
	Images     []fal.Image `json:"images"`
	ActualCost float64     `json:"actual_cost"`
}

func buildAsyncImageResultResponse(task *service.AsyncMediaTask) asyncImageResultResponse {
	response := asyncImageResultResponse{Status: modelAPIStatusCompleted, Images: make([]fal.Image, 0)}
	if task == nil {
		return response
	}

	response.ActualCost = task.FinalCost
	for i, imageURL := range task.ResultURLs() {
		item := fal.Image{URL: imageURL}
		if i < len(task.ImageMetadata) {
			metadata := task.ImageMetadata[i]
			item.ContentType = metadata.ContentType
			item.FileName = metadata.FileName
			item.FileSize = metadata.FileSize
			item.Width = metadata.Width
			item.Height = metadata.Height
		}
		response.Images = append(response.Images, item)
	}
	return response
}
