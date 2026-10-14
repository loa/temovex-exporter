package temovex

import (
	"bufio"
	"encoding/binary"
	"math"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specEncode implements the EXOline-TCP client frame independently of encode.
func specEncode(payload []byte) []byte {
	var checksum byte
	for _, b := range payload {
		checksum ^= b
	}
	out := []byte{0x3c}
	out = append(out, specEscape(append(append([]byte{}, payload...), checksum))...)
	out = append(out, 0x3e)
	return out
}

func specEscape(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b == 0x1b || b == 0x3c || b == 0x3d || b == 0x3e {
			out = append(out, 0x1b, b^0xff)
			continue
		}
		out = append(out, b)
	}
	return out
}

func TestEncodeMatchesSpecAndDocumentation(t *testing.T) {
	// Wire frames published in docs/modbus-signals.md.
	documented := []struct {
		name    string
		payload []byte
		wire    []byte
		call    func(*Client) (float64, error)
	}{
		{
			name:    "set",
			payload: []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x00, 0x08},
			wire:    []byte{0x3c, 0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x00, 0x08, 0x97, 0x3e},
			call:    (*Client).GetSet,
		},
		{
			name:    "al",
			payload: []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x3b, 0x00, 0x95},
			wire:    []byte{0x3c, 0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x3b, 0x00, 0x95, 0x35, 0x3e},
			call:    (*Client).GetAL,
		},
		{
			name:    "fl",
			payload: []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x08, 0x00},
			wire:    []byte{0x3c, 0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x08, 0x00, 0x97, 0x3e},
			call:    (*Client).GetFL,
		},
		{
			name:    "ul",
			payload: []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x04, 0xec},
			wire:    []byte{0x3c, 0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x04, 0x04, 0xec, 0x77, 0x3e},
			call:    (*Client).GetUL,
		},
		{
			name:    "tl",
			payload: []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x39, 0x02, 0xfa},
			wire:    []byte{0x3c, 0xff, 0x1e, 0xc8, 0x04, 0xb6, 0x39, 0x02, 0xfa, 0x5a, 0x3e},
			call:    (*Client).GetTL,
		},
	}

	response := []byte{0x3d, 0x05, 0x00, 0x00, 0x00, 0x98, 0x41, 0xdc, 0x3e}

	for _, tc := range documented {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wire, encode(tc.payload))
			assert.Equal(t, tc.wire, specEncode(tc.payload))
			assert.Equal(t, []byte{0xff, 0x1e, 0xc8, 0x04, 0xb6}, tc.payload[:5])

			got, val, err := exchange(t, tc.call, response)
			require.NoError(t, err)
			assert.Equal(t, tc.wire, got)
			assert.Equal(t, float64(19), val)
		})
	}
}

func exchange(t *testing.T, call func(*Client) (float64, error), response []byte) ([]byte, float64, error) {
	t.Helper()
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	deadline := time.Now().Add(2 * time.Second)
	require.NoError(t, srv.SetDeadline(deadline))
	require.NoError(t, cli.SetDeadline(deadline))

	type captured struct {
		got []byte
		err error
	}
	ch := make(chan captured, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := srv.Read(buf)
		if err != nil {
			ch <- captured{err: err}
			return
		}
		_, err = srv.Write(response)
		ch <- captured{got: append([]byte{}, buf[:n]...), err: err}
	}()

	client := &Client{Conn: cli, Reader: bufio.NewReader(cli)}
	val, err := call(client)
	capd := <-ch
	if capd.err != nil {
		return capd.got, val, capd.err
	}
	return capd.got, val, err
}

func TestParameterCellEncoding(t *testing.T) {
	// Spec: high = cell/60 and low = cell%60, so the low byte is 0–59.
	assert.Equal(t, []byte{0x00, 0x08}, []byte{8 / 60, 8 % 60})
	assert.Equal(t, []byte{0x08, 0x00}, []byte{480 / 60, 480 % 60})

	// Stored low bytes that the cell%60 encoding cannot emit.
	for _, low := range []byte{0xec, 0xfa, 0x95} {
		assert.Greater(t, low, byte(59))
	}
}

func TestSpecTemperatureExample(t *testing.T) {
	// Published MULCMD temperature reply. Checksum 0x3D is stuffed because it equals SOA.
	wire := []byte{0x3d, 0x05, 0x00, 0x14, 0xc3, 0xae, 0x41, 0x1b, 0xc2, 0x3e}
	msg, err := decode(wire)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x3d, 0x05, 0x00, 0x14, 0xc3, 0xae, 0x41, 0x3d, 0x3e}, msg)

	got, err := parseFloat(msg[3 : len(msg)-2])
	require.NoError(t, err)
	want := float64(math.Float32frombits(binary.LittleEndian.Uint32([]byte{0x14, 0xc3, 0xae, 0x41})))
	assert.Equal(t, want, got)
	assert.InDelta(t, 21.845, got, 0.001)
}

func TestDecodeRejectsSpecViolations(t *testing.T) {
	_, err := decode([]byte{0x3d, 0x05, 0x00, 0x00, 0x00, 0x98, 0x41, 0x00, 0x3e})
	assert.EqualError(t, err, "invalid checksum")

	_, err = decode([]byte{0x3c, 0x05, 0x00, 0x05, 0x3e})
	assert.EqualError(t, err, "invalid frame")

	_, err = decode([]byte{0x3d, 0x00, 0x1b})
	assert.EqualError(t, err, "truncated escape")

	_, err = parseFloat([]byte{0x00, 0x00})
	assert.EqualError(t, err, "expected 4 byte float, got 2")
}

func TestErrorResponseDoesNotPanic(t *testing.T) {
	// SOA, NAK 0x15, checksum 0x15, EOM.
	nak := []byte{0x3d, 0x15, 0x15, 0x3e}
	msg, err := decode(nak)
	require.NoError(t, err)
	assert.Equal(t, nak, msg)

	_, _, err = exchange(t, (*Client).GetSet, nak)
	assert.EqualError(t, err, "controller error 0x15: variable not found or access denied")
}
