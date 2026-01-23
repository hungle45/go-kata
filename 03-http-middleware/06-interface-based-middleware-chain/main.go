package main

import (
	"context"
	"log"
	"time"
)

func main() {
	processor := NewPipeline().
		WithMetrics().
		Then(NewTimeoutProcessorBuilder(10 * time.Second)).
		Then(NewValidatorProcessorBuilder()).
		Then(NewLoggerProcessorBuilder()).
		Then(NewEventSplitterProcessorBuilder(
			WithSplitRule(ActionUploadFile, []Action{ActionUploadToStorage, ActionUploadMetadata}),
		)).
		Build(NewStorageProcessor)
	event := NewEvent("user123", ActionUploadFile)
	resultEvents, err := processor.Process(context.Background(), event)
	if err != nil {
		log.Default().Println("Error processing event:", err)
		return
	}
	log.Default().Println("Successfully processed events:", resultEvents)
}
