package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type Processor interface {
	Process(ctx context.Context, event Event) ([]Event, error)
}

type ProcessorFunc func(ctx context.Context, event Event) ([]Event, error)

func (f ProcessorFunc) Process(ctx context.Context, event Event) ([]Event, error) {
	return f(ctx, event)
}

type Pipeline struct {
	builders      []ProcessBuilder
	enableMetrics bool
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		builders: []ProcessBuilder{},
	}
}

func (p *Pipeline) WithMetrics() *Pipeline {
	p.enableMetrics = true
	return p
}

func (p *Pipeline) Then(next ProcessBuilder) *Pipeline {
	p.builders = append(p.builders, next)
	return p
}

func (p *Pipeline) Build(final ConsumerBuilder) Processor {
	stageID := 0
	processor := p.wrapWithMetrics(&stageID, final())
	for i := len(p.builders) - 1; i >= 0; i-- {
		processor = p.wrapWithMetrics(&stageID, p.builders[i](processor))
	}
	return processor
}

func (p *Pipeline) wrapWithMetrics(stageID *int, processor Processor) Processor {
	*(stageID) = *stageID + 1
	if !p.enableMetrics {
		return processor
	}
	return NewMetricsProcessor(*stageID, processor)
}

type ProcessBuilder func(next Processor) Processor
type ConsumerBuilder func() Processor

func NewMetricsProcessor(stageID int, next Processor) Processor {
	return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
		if IsCtxDone(ctx) {
			log.Default().Println("[Metrics] Context done before processing event:", event.String())
			return nil, ctx.Err()
		}

		start := time.Now()
		events, err := next.Process(ctx, event)
		duration := time.Since(start)

		log.Default().Printf("[%d] Processed event in %v\n", stageID, duration)
		return events, err
	})
}

func NewValidatorProcessorBuilder() ProcessBuilder {
	return func(next Processor) Processor {
		return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
			if IsCtxDone(ctx) {
				log.Default().Println("[Validator] Context done before processing event:", event.String())
				return nil, ctx.Err()
			}

			if event.UserID == "" {
				return nil, ErrInvalidEvent
			}
			return next.Process(ctx, event)
		})
	}
}

func NewTimeoutProcessorBuilder(timeout time.Duration) ProcessBuilder {
	return func(next Processor) Processor {
		return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
			if IsCtxDone(ctx) {
				log.Default().Println("[Timeout] Context done before processing event:", event.String())
				return nil, ctx.Err()
			}

			ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next.Process(ctxWithTimeout, event)
		})
	}
}

func NewLoggerProcessorBuilder() ProcessBuilder {
	return func(next Processor) Processor {
		return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
			if IsCtxDone(ctx) {
				log.Default().Println("[Logger] Context done before processing event:", event.String())
				return nil, ctx.Err()
			}

			log.Default().Println("<-- ", event)
			events, err := next.Process(ctx, event)
			if err != nil {
				log.Default().Println("--> ", err)
			} else {
				log.Default().Println("--> ", events)
			}
			return events, err
		})
	}
}

type SplitterConfig struct {
	splitRules map[Action][]Action
}

type SplitterOption func(*SplitterConfig)

func WithSplitRule(action Action, splits []Action) SplitterOption {
	return func(cfg *SplitterConfig) {
		cfg.splitRules[action] = splits
	}
}

func NewEventSplitterProcessorBuilder(opts ...SplitterOption) ProcessBuilder {
	return func(next Processor) Processor {
		cfg := &SplitterConfig{
			splitRules: make(map[Action][]Action),
		}
		for _, opt := range opts {
			opt(cfg)
		}

		return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
			if IsCtxDone(ctx) {
				log.Default().Println("[EventSplitter] Context done before processing event:", event.String())
				return nil, ctx.Err()
			}

			events := make([]Event, 0)
			if splitActions, ok := cfg.splitRules[event.Action]; ok {
				for _, action := range splitActions {
					events = append(events, NewEvent(event.UserID, action))
				}
			} else {
				events = append(events, event)
			}

			var resultEvents []Event
			var resultErrors []error

			for _, evt := range events {
				processedEvents, err := next.Process(ctx, evt)
				if err != nil {
					resultErrors = append(resultErrors, err)
				} else {
					resultEvents = append(resultEvents, processedEvents...)
				}
			}

			return resultEvents, errors.Join(resultErrors...)
		})
	}
}

func NewStorageProcessor() Processor {
	return ProcessorFunc(func(ctx context.Context, event Event) ([]Event, error) {
		if IsCtxDone(ctx) {
			log.Default().Println("[Storage] Context done before processing event:", event.String())
			return nil, ctx.Err()
		}
		log.Default().Printf("Store %s\n", event.String())
		return []Event{event}, nil
	})
}

type Event struct {
	UserID string
	Action Action
}

func NewEvent(userID string, action Action) Event {
	return Event{
		UserID: userID,
		Action: action,
	}
}

func (e Event) String() string {
	return "Event{UserID: " + e.UserID + ", Action: " + fmt.Sprint(e.Action) + "}"
}

type Action int

const (
	ActionUploadFile Action = iota
	ActionUploadToStorage
	ActionUploadMetadata
)

func (action Action) String() string {
	switch action {
	case ActionUploadFile:
		return "UploadFile"
	case ActionUploadToStorage:
		return "UploadToStorage"
	case ActionUploadMetadata:
		return "UploadMetadata"
	default:
		return "UnknownAction"
	}
}

var (
	ErrInvalidEvent = errors.New("invalid event")
)

func IsCtxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
