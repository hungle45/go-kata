package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
)

const (
	SensorIDKey = "sensor_id"
	ReadingsKey = "readings"
)

type SensorData struct {
	SensorID string
	Value    float64 // first reading value
}
type SensorParser struct {
	r   io.Reader
	dec *json.Decoder
}

func NewSensorParser(r io.Reader) *SensorParser {
	return &SensorParser{
		r:   r,
		dec: json.NewDecoder(r),
	}
}

func (sp *SensorParser) Parse(ctx context.Context) (*SensorData, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		t, err := sp.dec.Token()
		if err == io.EOF {
			return nil, io.EOF
		}
		if err != nil {
			sp.resync()
			continue
		}

		if delim, ok := t.(json.Delim); !ok || delim != '{' {
			continue
		}

		data, err := sp.parseObject()
		if err != nil {
			sp.resync()
			continue
		}

		return data, nil
	}
}

func (sp *SensorParser) resync() {
	source := io.MultiReader(sp.dec.Buffered(), sp.r)

	buf := make([]byte, 1)
	for {
		_, err := source.Read(buf)
		if err != nil {
			return
		}

		if buf[0] == '{' {
			sp.dec = json.NewDecoder(io.MultiReader(bytes.NewReader(buf), source))
			return
		}
	}
}

func (sp *SensorParser) parseObject() (*SensorData, error) {
	depth := 1
	data := &SensorData{}
	hasSensorID := false
	hasReadings := false

	for depth > 0 {
		t, err := sp.dec.Token()
		if err != nil {
			return nil, err
		}

		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		case string:
			switch v {
			case SensorIDKey:
				t, err := sp.dec.Token()
				if err != nil {
					return nil, err
				}
				if sensorID, ok := t.(string); ok {
					data.SensorID = sensorID
					hasSensorID = true
				}
			case ReadingsKey:
				t, err := sp.dec.Token()
				if err != nil {
					return nil, err
				}

				if delim, ok := t.(json.Delim); !ok || delim != '[' {
					return nil, errors.New("unexpected end of JSON input")
				}

				depth++
				t, err = sp.dec.Token()
				if err != nil {
					return nil, err
				}
				if value, ok := t.(float64); ok {
					data.Value = value
					hasReadings = true
				}
			}
		}
		if depth == 0 {
			break
		}
	}
	if hasSensorID && hasReadings {
		return data, nil
	}
	return nil, errors.New("no valid sensor data found")
}
