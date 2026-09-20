package llamacpp

import (
	"strings"
)

// ChatSession 在 Go 侧维护 messages，适配 ServerEngine/LocalEngine 的“对话态”。
// 对于 LocalEngine，会触发增量 eval（如果新 prompt 是上一轮的前缀扩展）。
type ChatSession struct {
	eng      Engine
	messages []Message
	opt      Options
}

func NewChatSession(eng Engine, opt Options) *ChatSession {
	opt.applyDefaults()
	s := &ChatSession{
		eng: eng,
		opt: opt,
	}
	// 默认带 system
	if strings.TrimSpace(opt.SystemPrompt) != "" {
		s.messages = append(s.messages, Message{Role: "system", Content: opt.SystemPrompt})
	}
	return s
}

func (s *ChatSession) Messages() []Message {
	out := make([]Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *ChatSession) Reset() error {
	s.messages = nil
	if strings.TrimSpace(s.opt.SystemPrompt) != "" {
		s.messages = append(s.messages, Message{Role: "system", Content: s.opt.SystemPrompt})
	}
	return s.eng.Reset()
}

func (s *ChatSession) Send(user string) (string, error) {
	s.messages = append(s.messages, Message{Role: "user", Content: user})
	out, err := s.eng.Chat(s.messages, s.opt)
	if err != nil {
		return "", err
	}
	s.messages = append(s.messages, Message{Role: "assistant", Content: out})
	return out, nil
}
