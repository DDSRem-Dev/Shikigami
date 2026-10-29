package client

// InputDecoder recognizes bracketed paste across arbitrary Read boundaries.
// Bytes are forwarded immediately; an isolated Escape is never delayed. The
// detach byte is intercepted only outside a paste, and trailing bytes discarded.
type InputDecoder struct {
	Detach  byte
	paste   bool
	matched int
}

func (d *InputDecoder) Feed(input []byte) ([]byte, bool) {
	output := make([]byte, 0, len(input))
	for _, b := range input {
		if d.Detach != 0 && b == d.Detach && !d.paste {
			return output, true
		}
		output = append(output, b)
		marker := []byte("\x1b[200~")
		if d.paste {
			marker = []byte("\x1b[201~")
		}
		if b == marker[d.matched] {
			d.matched++
			if d.matched == len(marker) {
				d.paste = !d.paste
				d.matched = 0
			}
		} else if b == marker[0] {
			d.matched = 1
		} else {
			d.matched = 0
		}
	}
	return output, false
}
