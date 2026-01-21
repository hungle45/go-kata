package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// 1. Functional & Corruption Tests (Table-Driven)
func TestSensorParser_Parse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []SensorData
	}{
		{
			name:  "Single valid object",
			input: `{"sensor_id": "temp-1", "readings": [22.1, 22.3]}`,
			expected: []SensorData{
				{SensorID: "temp-1", Value: 22.1},
			},
		},
		{
			name: "Multiple valid objects",
			input: `
				{"sensor_id": "temp-1", "readings": [22.1]}
				{"sensor_id": "temp-2", "readings": [23.1]}
			`,
			expected: []SensorData{
				{SensorID: "temp-1", Value: 22.1},
				{SensorID: "temp-2", Value: 23.1},
			},
		},
		{
			name: "Corruption recovery (The Corruption Test)",
			input: `
				{"sensor_id": "good-1", "readings": [10.0]}
				{BROKEN JSON HERE
				{"sensor_id": "good-2", "readings": [20.0]}
			`,
			expected: []SensorData{
				{SensorID: "good-1", Value: 10.0},
				{SensorID: "good-2", Value: 20.0},
			},
		},
		{
			name: "Skip objects missing required fields",
			input: `
				{"sensor_id": "missing-readings"}
				{"readings": [30.0]}
				{"sensor_id": "good-3", "readings": [30.0]}
			`,
			expected: []SensorData{
				{SensorID: "good-3", Value: 30.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			parser := NewSensorParser(r)
			ctx := context.Background()

			var results []SensorData
			for {
				data, err := parser.Parse(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					// If Parse returns an error, it means it couldn't recover or stream ended badly.
					// For these tests, we expect Parse to recover internally and only return valid data or EOF.
					t.Fatalf("Unexpected error during parse: %v", err)
				}
				results = append(results, *data)
			}

			if len(results) != len(tt.expected) {
				t.Errorf("Expected %d results, got %d", len(tt.expected), len(results))
			}
			for i := range results {
				if i >= len(tt.expected) {
					break
				}
				if results[i] != tt.expected[i] {
					t.Errorf("Result %d: expected %+v, got %+v", i, tt.expected[i], results[i])
				}
			}
		})
	}
}

// 2. The Allocation Test
func BenchmarkSensorParser_Parse(b *testing.B) {
	input := `{"sensor_id": "bench-1", "timestamp": 1234567890, "readings": [22.1, 22.3, 22.0], "metadata": {"foo": "bar"}}`

	// Pre-generate enough data for the benchmark to avoid reader overhead dominating
	// or running out of data.
	// Note: In a real stream, we'd have infinite data. Here we simulate it.
	data := []byte(strings.Repeat(input+"\n", b.N+1))
	r := bytes.NewReader(data)

	parser := NewSensorParser(r)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs() // This is crucial for the "Zero-Allocation" check

	for i := 0; i < b.N; i++ {
		_, err := parser.Parse(ctx)
		if err != nil {
			if err == io.EOF {
				break
			}
			b.Fatal(err)
		}
	}
}

// 3. The Stream Test (Large Input)
func TestSensorParser_LargeStream(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large stream test in short mode")
	}

	// Simulate a large stream (e.g., 10MB) to ensure buffer reuse works
	// and we don't lose data during resyncs (if we inject errors).

	// We'll use a smaller size than 1GB for unit tests to keep it fast,
	// but large enough to exceed default buffer sizes (usually 4KB).
	const numRecords = 10000
	input := `{"sensor_id": "stream", "readings": [1.0]}` + "\n"

	// Create a reader that repeats this input
	r := &RepeatingReader{
		Data:  []byte(input),
		Count: numRecords,
	}

	parser := NewSensorParser(r)
	ctx := context.Background()

	count := 0
	for {
		_, err := parser.Parse(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Stream failed at record %d: %v", count, err)
		}
		count++
	}

	if count != numRecords {
		t.Errorf("Expected %d records, got %d. Did we lose data due to bad resync?", numRecords, count)
	}
}

func TestSensorParser_Resync_BufferLoss(t *testing.T) {
	valid1 := `{"sensor_id": "id-1", "readings": [1.0]}`
	garbage := ` {BROKEN_JSON`
	valid2 := `{"sensor_id": "id-2", "readings": [2.0]}`

	padding := strings.Repeat(" ", 5000)

	input := valid1 + "\n" + garbage + "\n" + padding + "\n" + valid2

	r := strings.NewReader(input)
	parser := NewSensorParser(r)
	ctx := context.Background()

	var results []SensorData
	for {
		data, err := parser.Parse(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Logf("Parse returned error: %v", err)
			continue
		}
		results = append(results, *data)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 records, got %d. \nFound: %+v\nExplanation: If you got 1, it means 'id-2' was lost because resync() discarded the unbuffered part of the stream.", len(results), results)
	} else {
		if results[1].SensorID != "id-2" {
			t.Errorf("Expected second record to be id-2, got %s", results[1].SensorID)
		}
	}
}

// RepeatingReader helper for stream test
type RepeatingReader struct {
	Data  []byte
	Count int
	cur   int // current record index
	off   int // offset in Data
}

func (r *RepeatingReader) Read(p []byte) (n int, err error) {
	if r.cur >= r.Count {
		return 0, io.EOF
	}

	copied := 0
	for copied < len(p) {
		if r.cur >= r.Count {
			break
		}
		n := copy(p[copied:], r.Data[r.off:])
		r.off += n
		copied += n
		if r.off >= len(r.Data) {
			r.off = 0
			r.cur++
		}
	}

	if copied == 0 && r.cur >= r.Count {
		return 0, io.EOF
	}
	return copied, nil
}
