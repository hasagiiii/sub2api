package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

var ErrQueuedImageNotStarted = errors.New("image request was not sent upstream")

type openAIImagesTaskStorageKey struct{}

func taskOwnsImageStorage(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(openAIImagesTaskStorageKey{}).(bool)
	return value
}

// ForwardQueuedImages reserves a real account slot for work that outlives the HTTP request.
// The task executor alone stores images and publishes completion for this route.
func (s *OpenAIGatewayService) ForwardQueuedImages(ctx context.Context, c *gin.Context, account *Account, body []byte, parsed *OpenAIImagesRequest) (*OpenAIForwardResult, error) {
	for {
		slot, err := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrQueuedImageNotStarted, err)
		}
		if slot != nil && slot.Acquired {
			if slot.ReleaseFunc != nil {
				defer slot.ReleaseFunc()
			}
			snapshot := *account
			snapshot.Credentials, err = cloneAccountJSONMap(account.Credentials)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrQueuedImageNotStarted, err)
			}
			snapshot.Extra, err = cloneAccountJSONMap(account.Extra)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrQueuedImageNotStarted, err)
			}
			account = &snapshot
			ctx = WithOpenAIImagesAccountSlot(ctx, account.ID)
			ctx = context.WithValue(ctx, openAIImagesTaskStorageKey{}, true)
			c.Request = c.Request.WithContext(ctx)
			return s.ForwardImages(ctx, c, account, body, parsed, "")
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%w: %w", ErrQueuedImageNotStarted, ctx.Err())
		case <-timer.C:
		}
	}
}
