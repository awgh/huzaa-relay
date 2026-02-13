package turnrelay

import (
	"encoding/binary"
	"io"
)

// Message types (bot <-> relay).
const (
	MsgRegisterDownload = 0x01
	MsgRegisterUpload   = 0x02
	MsgPortAlloc        = 0x03
	MsgData             = 0x04
	MsgError            = 0x05
	MsgEOF              = 0x06
)

// Frame: 1 byte type + 4 byte length (big-endian) + payload.
func ReadFrame(r io.Reader) (msgType byte, payload []byte, err error) {
	var h [5]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	msgType = h[0]
	ln := binary.BigEndian.Uint32(h[1:5])
	if ln > 2*1024*1024 { // 2MB max payload
		return 0, nil, io.ErrShortBuffer
	}
	payload = make([]byte, ln)
	if ln > 0 {
		if _, err = io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return msgType, payload, nil
}

func WriteFrame(w io.Writer, msgType byte, payload []byte) error {
	var h [5]byte
	h[0] = msgType
	binary.BigEndian.PutUint32(h[1:5], uint32(len(payload)))
	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}
