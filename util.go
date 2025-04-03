package memlist

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
)

// remove the element from the string slice at the given index.
func remove(s []string, i int) []string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

// insert a string into the slice with the given value and at the index.
func insert(a []string, index int, value string) []string {
	if len(a) == index { // nil or empty slice or after last element
		return append(a, value)
	}
	a = append(a[:index+1], a[index:]...) // index < len(a)
	a[index] = value
	return a
}

// encodeMessage will encode the provided type into a byte slice and appends the
// message type to the front of the byte slice.
func encodeMessage(tp messageType, e interface{}) ([]byte, error) {
	buf := bytes.NewBuffer([]byte{uint8(tp)})
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	packetSize := i32tob(uint32(buf.Len()))
	return append(packetSize, buf.Bytes()...), nil
}

func i32tob(val uint32) []byte {
	r := make([]byte, 4)
	binary.LittleEndian.PutUint32(r, val)
	return r
}
