package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"
)

func main() {
	const jsonStream = `
	{"sensor_id": "temp-1", "timestamp": 1234567890, "readings": [22.1, 22.3, 22.0], "metadata": {}}
	{"sensor_id": "temp-2", "timestamp": 1234567890, "readings": [22.2, 22.3, 22.0], "metadata": {
	{"sensor_id": "temp-3", "timestamp": 1234567890, "readings": [22.3, 22.3, 22.0], "metadata": {}}
	{"sensor_id": "temp-4", "timestamp": 1234567890, "readings": [22.4, 22.3, 22.0], "metadata": {}}
	`

	parser := NewSensorParser(strings.NewReader(jsonStream))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for {
		data, err := parser.Parse(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatalf("Error parsing sensor data: %v", err)
		} else {
			fmt.Printf("Parsed Sensor Data: %+v\n", data)
		}
	}
}
