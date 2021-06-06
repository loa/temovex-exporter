package temovex

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"time"
)

// Client for Temovex
type Client struct {
	Conn   net.Conn
	Reader *bufio.Reader
}

// NewClient creates new Temovex client
func NewClient(address string) (*Client, error) {
	client := Client{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var dialer net.Dialer
	var err error
	if client.Conn, err = dialer.DialContext(ctx, "tcp", address); err != nil {
		return nil, err
	}
	client.Reader = bufio.NewReader(client.Conn)

	return &client, nil
}

// GetSet temperature
func (client *Client) GetSet() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x00, 0x08})
}

// GetUL temperature
func (client *Client) GetUL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x04, 0xec})
}

// GetTL temperature
func (client *Client) GetTL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x39, 0x02, 0xfa})
}

// GetFL temperature
func (client *Client) GetFL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x08, 0x00})
}

// GetAL temperature
func (client *Client) GetAL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x3b, 0x00, 0x95})
}

func (client *Client) getFloat(msg []byte) (float64, error) {
	if err := client.send(msg); err != nil {
		return 0.0, err
	}

	msg, err := client.read()
	if err != nil {
		return 0.0, err
	}

	return parseFloat(msg[3 : len(msg)-2])
}

func (client *Client) send(data []byte) error {
	_, err := client.Conn.Write(encode(data))
	return err
}

func (client *Client) read() ([]byte, error) {
	// read until end byte
	msg, err := client.Reader.ReadBytes(0x3e)
	if err != nil {
		return []byte{}, err
	}

	return decode(msg)
}

func encode(msg []byte) []byte {
	// calculate checksum
	var checksum byte
	for _, v := range msg {
		checksum ^= v
	}

	// add client start
	out := []byte{0x3c}

	// add message
	out = append(out, escape(msg)...)

	// add checksum
	out = append(out, escape([]byte{checksum})...)

	// add end byte
	out = append(out, 0x3e)

	return out
}

func decode(msg []byte) ([]byte, error) {
	// unescape
	msg = unescape(msg)

	payload := msg[1 : len(msg)-2]
	providedChecksum := msg[len(msg)-2]

	// calculate checksum
	var checksum byte
	for _, p := range payload {
		checksum ^= p
	}
	// verify checksum
	if providedChecksum != checksum {
		return []byte{}, fmt.Errorf("invalid checksum")
	}

	return msg, nil
}

func escape(payload []byte) []byte {
	res := []byte{}

	for _, p := range payload {
		if p == 0x3c || p == 0x3d || p == 0x1b || p == 0x3e {
			// escape bytes with equal values of start, end, escape
			res = append(res, 0x1b, p^0xff)
		} else {
			res = append(res, p)
		}
	}

	return res
}

func unescape(payload []byte) []byte {
	res := []byte{}

	for i := 0; i < len(payload); i++ {
		if payload[i] == 0x1b {
			// if escape is found, skip escape and invert following byte
			i++
			res = append(res, payload[i]^0xff)
		} else {
			res = append(res, payload[i])
		}
	}

	return res
}

func parseFloat(b []byte) (float64, error) {
	// parse float from little endian bytes
	i := binary.LittleEndian.Uint32(b)
	return float64(math.Float32frombits(i)), nil
}
