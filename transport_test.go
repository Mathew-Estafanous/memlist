package memlist

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestNetTransport_SendTo(t *testing.T) {
	transport, err := NewNetTransport("127.0.0.1", 8080)
	require.NoError(t, err)

	udpAddr := &net.UDPAddr{Port: 8090, IP: net.ParseIP("127.0.0.1")}
	udpCon, err := net.ListenUDP("udp", udpAddr)

	msg := []byte{'h', 'e', 'l', 'l', 'o'}
	received := make(chan struct{})
	go func() {
		b := make([]byte, 5)
		_, addr, err := udpCon.ReadFrom(b)
		assert.NoError(t, err)
		assert.Equal(t, "127.0.0.1:8080", addr.String())
		assert.Equal(t, b, msg)
		received <- struct{}{}
	}()
	err = transport.SendTo(msg, "127.0.0.1:8090")
	assert.NoError(t, err)

	select {
	case <-received:
	case <- time.After(1 * time.Second):
		t.Fatalf("Failed to receive UDP packet within a reasonable time.")
	}
}

func TestNetTransport_DialAndConnect(t *testing.T) {
	transport, err := NewNetTransport("127.0.0.1", 8080)
	require.NoError(t, err)

	tcpAddr := &net.TCPAddr{Port: 8090, IP: net.ParseIP("127.0.0.1")}
	tcpCon, err := net.ListenTCP("tcp", tcpAddr)
	require.NoError(t, err)

	received := make(chan struct{})
	go func() {
		_, err := tcpCon.AcceptTCP()
		assert.NoError(t, err)
		received <- struct{}{}
	}()
	_, err = transport.DialAndConnect(tcpAddr.String(), 500 * time.Millisecond)
	assert.NoError(t, err)

	select {
	case <-received:
	case <- time.After(500 * time.Millisecond):
		t.Fatalf("Failed to receive TCP connection within a reasonable time.")
	}
}
