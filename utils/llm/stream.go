package llm

import (
	"errors"
	"io"
)

type StreamDelta struct {
	Content          string
	ReasoningContent string
	FinishReason     string
}

// GenerateMessagesStream consumes the official SDK stream through the shared
// provider interface. No endpoint-specific SSE parsing lives in this facade.
func GenerateMessagesStream(messages []Message, opt GenerateOptions, onDelta func(StreamDelta) error) error {
	if onDelta == nil {
		return errors.New("stream callback is required")
	}
	provider, request, ctx, cancel, err := prepareRequest(messages, opt)
	if err != nil {
		return err
	}
	defer cancel()

	stream, err := provider.Stream(ctx, request)
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		delta := StreamDelta{Content: event.ContentDelta, ReasoningContent: event.ReasoningDelta, FinishReason: event.FinishReason}
		if delta.Content != "" || delta.ReasoningContent != "" || delta.FinishReason != "" {
			if err := onDelta(delta); err != nil {
				return err
			}
		}
		if delta.FinishReason == "length" {
			return &OutputLimitError{}
		}
	}
}
