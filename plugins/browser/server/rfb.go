package server

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"math/bits"
)

const maxClipboard = 1 << 20
const maxEncodings = 256
const maxScreens = 16

// clientMessage frames every accepted client-to-server RFB 3.8 message before
// forwarding. Unknown extensions fail closed, never become an unchecked pipe.
func clientMessage(reader io.Reader) ([]byte, bool, error) {
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil {
		return nil, false, err
	}
	message := []byte{first[0]}
	read := func(length int) error {
		start := len(message)
		message = append(message, make([]byte, length)...)
		_, err := io.ReadFull(reader, message[start:])
		return err
	}
	mutating := false
	switch first[0] {
	case 0: // SetPixelFormat
		if err := read(19); err != nil {
			return nil, false, err
		}
		if !validPixelFormat(message[4:20]) {
			return nil, false, errors.New("unsupported or invalid RFB true-colour pixel format")
		}
	case 2: // SetEncodings (also negotiates extension capabilities)
		if err := read(3); err != nil {
			return nil, false, err
		}
		count := int(binary.BigEndian.Uint16(message[2:4]))
		if count > maxEncodings {
			return nil, false, errors.New("RFB encoding count exceeds bound")
		}
		if err := read(count * 4); err != nil {
			return nil, false, err
		}
	case 3: // FramebufferUpdateRequest
		if err := read(9); err != nil {
			return nil, false, err
		}
		if !boundedSize(binary.BigEndian.Uint16(message[6:8]), binary.BigEndian.Uint16(message[8:10])) {
			return nil, false, errors.New("RFB update dimensions exceed bound")
		}
	case 4: // KeyEvent
		mutating = true
		if err := read(7); err != nil {
			return nil, false, err
		}
		if message[1] > 1 {
			return nil, false, errors.New("invalid RFB key state")
		}
	case 5: // PointerEvent, including negotiated extended mouse buttons
		mutating = true
		if err := read(5); err != nil {
			return nil, false, err
		}
		if message[1]&128 != 0 {
			if err := read(1); err != nil {
				return nil, false, err
			}
		}
	case 6: // ClientCutText, including ExtendedClipboard's negative length
		mutating = true
		if err := read(7); err != nil {
			return nil, false, err
		}
		length := int64(int32(binary.BigEndian.Uint32(message[4:8])))
		extended := length < 0
		if extended {
			length = -length
		}
		if length > maxClipboard {
			return nil, false, errors.New("RFB clipboard exceeds bound")
		}
		if err := read(int(length)); err != nil {
			return nil, false, err
		}
		if extended {
			if err := validateExtendedClipboard(message[8:]); err != nil {
				return nil, false, err
			}
		}
	case 150: // EnableContinuousUpdates
		if err := read(9); err != nil {
			return nil, false, err
		}
		if message[1] > 1 || !boundedSize(binary.BigEndian.Uint16(message[6:8]), binary.BigEndian.Uint16(message[8:10])) {
			return nil, false, errors.New("invalid RFB continuous update request")
		}
	case 248: // ClientFence, bounded synchronization, not an input event
		if err := read(8); err != nil {
			return nil, false, err
		}
		if message[8] > 64 {
			return nil, false, errors.New("RFB fence payload exceeds bound")
		}
		if err := read(int(message[8])); err != nil {
			return nil, false, err
		}
	case 251: // SetDesktopSize
		mutating = true
		if err := read(7); err != nil {
			return nil, false, err
		}
		if binary.BigEndian.Uint16(message[2:4]) == 0 || binary.BigEndian.Uint16(message[4:6]) == 0 || !boundedSize(binary.BigEndian.Uint16(message[2:4]), binary.BigEndian.Uint16(message[4:6])) || message[6] == 0 || message[6] > maxScreens {
			return nil, false, errors.New("invalid RFB desktop resize")
		}
		if err := read(int(message[6]) * 16); err != nil {
			return nil, false, err
		}
		var ids [maxScreens]uint32
		for screen := range int(message[6]) {
			offset := 8 + screen*16
			id := binary.BigEndian.Uint32(message[offset : offset+4])
			x, y := uint32(binary.BigEndian.Uint16(message[offset+4:offset+6])), uint32(binary.BigEndian.Uint16(message[offset+6:offset+8]))
			width, height := binary.BigEndian.Uint16(message[offset+8:offset+10]), binary.BigEndian.Uint16(message[offset+10:offset+12])
			if width == 0 || height == 0 || !boundedSize(width, height) || x+uint32(width) > uint32(binary.BigEndian.Uint16(message[2:4])) || y+uint32(height) > uint32(binary.BigEndian.Uint16(message[4:6])) {
				return nil, false, errors.New("RFB screen lies outside bounded desktop")
			}
			for prior := range screen {
				if ids[prior] == id {
					return nil, false, errors.New("duplicate RFB screen ID")
				}
			}
			ids[screen] = id
		}
	case 255: // QEMU ExtendedKeyEvent
		mutating = true
		if err := read(11); err != nil {
			return nil, false, err
		}
		if message[1] != 0 || binary.BigEndian.Uint16(message[2:4]) > 1 {
			return nil, false, errors.New("unsupported RFB QEMU extension")
		}
	default:
		return nil, false, errors.New("unrecognized RFB client message")
	}
	return message, mutating, nil
}
func boundedSize(width, height uint16) bool {
	return width <= 4096 && height <= 2160 && uint64(width)*uint64(height) <= 4096*2160
}

func validPixelFormat(format []byte) bool {
	if len(format) != 16 || (format[0] != 8 && format[0] != 16 && format[0] != 32) || format[1] == 0 || format[1] > format[0] || format[2] > 1 || format[3] != 1 {
		return false
	}
	var mask uint32
	depth := 0
	for channel := range 3 {
		maximum := binary.BigEndian.Uint16(format[4+channel*2 : 6+channel*2])
		shift := int(format[10+channel])
		width := bits.Len16(maximum)
		if maximum == 0 || maximum&(maximum+1) != 0 || shift+width > int(format[0]) {
			return false
		}
		current := uint32(maximum) << shift
		if mask&current != 0 {
			return false
		}
		mask |= current
		depth += width
	}
	return depth <= int(format[1])
}
func validateExtendedClipboard(data []byte) error {
	if len(data) < 4 {
		return errors.New("truncated RFB extended clipboard")
	}
	flags := binary.BigEndian.Uint32(data[:4])
	const formats = uint32(0xffff)
	const actions = uint32(0x1f000000)
	if flags & ^(formats|actions) != 0 || flags&actions == 0 {
		return errors.New("invalid RFB extended clipboard flags")
	}
	if flags&(1<<24) != 0 { // capabilities, per-format size hints
		count := 0
		for bit := range 16 {
			if flags&(1<<bit) != 0 {
				count++
			}
		}
		if len(data) != 4+count*4 {
			return errors.New("invalid RFB clipboard capabilities")
		}
		return nil
	}
	if flags&(1<<28) == 0 {
		if len(data) != 4 {
			return errors.New("invalid RFB clipboard control payload")
		}
		return nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(data[4:]))
	if err != nil {
		return errors.New("invalid compressed RFB clipboard")
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, maxClipboard+1))
	// RFB clipboard providers (including noVNC) may send a complete
	// Z_FULL_FLUSH block without terminating the zlib stream. Accept that
	// boundary only with its flush marker and complete format payloads below.
	flushed := errors.Is(err, io.ErrUnexpectedEOF) && bytes.HasSuffix(data[4:], []byte{0, 0, 0xff, 0xff})
	if (err != nil && !flushed) || len(decoded) > maxClipboard {
		return errors.New("expanded RFB clipboard exceeds bound")
	}
	offset := 0
	for bit := range 16 {
		if flags&(1<<bit) == 0 {
			continue
		}
		if len(decoded)-offset < 4 {
			return errors.New("truncated RFB clipboard format")
		}
		length := int64(binary.BigEndian.Uint32(decoded[offset : offset+4]))
		offset += 4
		if length > int64(len(decoded)-offset) {
			return errors.New("truncated RFB clipboard value")
		}
		offset += int(length)
	}
	if offset != len(decoded) {
		return errors.New("trailing RFB clipboard content")
	}
	return nil
}
