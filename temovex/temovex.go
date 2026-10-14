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

// Temperature reads are MULCMD (0xC8) wrapped RRP (0xB6) requests:
// PLA, ELA, 0xC8, length, opcode, line, cell high, cell low.
// The specification encodes a parameter cell as high = cell/60 and low = cell%60,
// so the low byte is 0–59. GetUL, GetTL, and GetAL keep the stored low bytes
// 0xEC, 0xFA, and 0x95.

// GetSet temperature. Line 4, cell 8.
func (client *Client) GetSet() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x00, 0x08})
}

// GetUL temperature. Line 4. Low address byte 0xEC is outside cell%60.
func (client *Client) GetUL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x04, 0xec})
}

// GetTL temperature. Line 0x39. Low address byte 0xFA is outside cell%60.
func (client *Client) GetTL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x39, 0x02, 0xfa})
}

// GetFL temperature. Line 4, cell 480.
func (client *Client) GetFL() (float64, error) {
	return client.getFloat([]byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x08, 0x00})
}

// GetAL temperature. Line 0x3B. Low address byte 0x95 is outside cell%60.
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

	// A MULCMD real reply is SOA, two header bytes, a float32, the checksum, and EOM.
	// Four bytes is the specification's error frame: SOA, code, checksum, EOM.
	if len(msg) == 4 {
		return 0.0, controllerError(msg[1])
	}
	// SOA + 2 header bytes + float32 + checksum + EOM.
	if len(msg) < 9 {
		return 0.0, fmt.Errorf("expected float response, got %d bytes", len(msg))
	}
	return parseFloat(msg[3 : len(msg)-2])
}

func controllerError(code byte) error {
	reasons := map[byte]string{
		0x15: "variable not found or access denied",
		0x40: "framing error",
		0x41: "checksum mismatch",
		0x42: "empty response",
		0x47: "logic value error",
		0x48: "index out of range",
		0x49: "integer out of range",
		0x50: "real value error",
		0x51: "string value error",
	}
	if reason, ok := reasons[code]; ok {
		return fmt.Errorf("controller error 0x%02x: %s", code, reason)
	}
	return fmt.Errorf("controller error 0x%02x", code)
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
	msg, err := unescape(msg)
	if err != nil {
		return nil, err
	}
	// Answer frames open with SOA and close with EOM. The shortest valid
	// answer is SOA, checksum, EOM.
	if len(msg) < 3 || msg[0] != 0x3d || msg[len(msg)-1] != 0x3e {
		return nil, fmt.Errorf("invalid frame")
	}

	payload := msg[1 : len(msg)-2]
	providedChecksum := msg[len(msg)-2]

	// XOR every byte after SOA and before the checksum. SOA and EOM are excluded.
	var checksum byte
	for _, p := range payload {
		checksum ^= p
	}
	if providedChecksum != checksum {
		return nil, fmt.Errorf("invalid checksum")
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

func unescape(payload []byte) ([]byte, error) {
	res := []byte{}

	for i := 0; i < len(payload); i++ {
		if payload[i] == 0x1b {
			// Skip the escape and restore the following byte with its complement.
			i++
			if i >= len(payload) {
				return nil, fmt.Errorf("truncated escape")
			}
			res = append(res, payload[i]^0xff)
		} else {
			res = append(res, payload[i])
		}
	}

	return res, nil
}

func parseFloat(b []byte) (float64, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("expected 4 byte float, got %d", len(b))
	}
	i := binary.LittleEndian.Uint32(b)
	return float64(math.Float32frombits(i)), nil
}
