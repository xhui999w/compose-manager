package docker

import (
	"encoding/binary"
	"testing"
)

func TestDemuxLogs(t *testing.T) {
	frame := func(stream byte, text string) []byte {
		header := make([]byte, 8)
		header[0] = stream
		binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
		return append(header, []byte(text)...)
	}
	data := append(frame(1, "hello\n"), frame(2, "error\n")...)
	if got := demuxLogs(data); got != "hello\nerror\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDemuxTTYLogs(t *testing.T) {
	if got := demuxLogs([]byte("plain\n")); got != "plain\n" {
		t.Fatalf("got %q", got)
	}
}
