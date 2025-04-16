package memlist

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestInsert(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		index    int
		value    string
		expected []string
	}{
		{"Insert into empty slice", []string{}, 0, "test", []string{"test"}},
		{"Insert at the beginning", []string{"a", "b", "c"}, 0, "start", []string{"start", "a", "b", "c"}},
		{"Insert in the middle", []string{"a", "b", "d"}, 2, "c", []string{"a", "b", "c", "d"}},
		{"Insert at the end", []string{"a", "b", "c"}, 3, "d", []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := insert(tt.input, tt.index, tt.value)
			assert.Equal(t, tt.expected, result, tt.name)
		})
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		index    int
		expected []string
	}{
		{"Remove from the beginning", []string{"a", "b", "c"}, 0, []string{"c", "b"}},
		{"Remove from the middle", []string{"a", "b", "c"}, 1, []string{"a", "c"}},
		{"Remove from the end", []string{"a", "b", "c"}, 2, []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := remove(tt.input, tt.index)
			assert.Equal(t, tt.expected, result, tt.name)
		})
	}
}

func TestI32tob(t *testing.T) {
	tests := []struct {
		name     string
		input    uint32
		expected []byte
	}{
		{"Convert 0", 0, []byte{0, 0, 0, 0}},
		{"Convert 1", 1, []byte{1, 0, 0, 0}},
		{"Convert 16909060", 16909060, []byte{4, 3, 2, 1}}, // 0x01020304 in hex
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := i32tob(tt.input)
			assert.Equal(t, tt.expected, result, tt.name)
		})
	}
}
