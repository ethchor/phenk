package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Streaming format constants.
//
// A raw message is capped at 25MB on the wire, and the SMTP path streams it to
// disk precisely so that ten simultaneous senders are not ten times that in
// heap. Encrypting with a single one-shot Seal would give all of that back, so
// the stream is framed instead and never more than one frame is held at once.
const (
	streamVersion   byte = 1
	streamChunkSize      = 64 * 1024
	streamPrefixLen      = 4
	streamNonceLen       = 12
)

// Frame trailers distinguish the last frame from every other one, so a
// truncated stream is detected rather than silently accepted as a short
// message.
const (
	frameContinues byte = 0
	frameFinal     byte = 1
)

// ErrStreamFormat is returned when an encrypted stream is malformed or
// truncated.
var ErrStreamFormat = errors.New("crypto: encrypted stream is malformed")

// SealStream encrypts src into dst and returns the number of plaintext bytes
// consumed.
func (d *DataKey) SealStream(dst io.Writer, src io.Reader) (int64, error) {
	if d.aead == nil {
		return 0, ErrKeyDestroyed
	}

	header := make([]byte, 1+streamPrefixLen)
	header[0] = streamVersion
	if _, err := rand.Read(header[1:]); err != nil {
		return 0, fmt.Errorf("crypto: random source unavailable: %w", err)
	}
	if _, err := dst.Write(header); err != nil {
		return 0, err
	}
	prefix := header[1:]

	var (
		plain    = make([]byte, streamChunkSize)
		sealed   = make([]byte, 0, streamChunkSize+d.aead.Overhead())
		lenBuf   [4]byte
		counter  uint64
		total    int64
		pending  []byte
		finished bool
	)

	// Read one frame ahead, so the last frame can be marked as final without
	// having to know the total length in advance.
	pending, finished, err := readChunk(src, plain)
	if err != nil {
		return 0, err
	}
	for {
		trailer := frameContinues
		if finished {
			trailer = frameFinal
		}
		nonce := streamNonce(prefix, counter)
		sealed = d.aead.Seal(sealed[:0], nonce, pending, []byte{trailer})

		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(sealed)))
		if _, err := dst.Write(lenBuf[:]); err != nil {
			return total, err
		}
		if _, err := dst.Write(sealed); err != nil {
			return total, err
		}
		total += int64(len(pending))
		counter++

		if finished {
			return total, nil
		}
		next := make([]byte, streamChunkSize)
		pending, finished, err = readChunk(src, next)
		if err != nil {
			return total, err
		}
	}
}

// OpenStream decrypts src into dst and returns the number of plaintext bytes
// produced. It returns an error if the stream was truncated, because a
// truncated message that decrypts cleanly would be worse than one that fails.
func (d *DataKey) OpenStream(dst io.Writer, src io.Reader) (int64, error) {
	if d.aead == nil {
		return 0, ErrKeyDestroyed
	}

	header := make([]byte, 1+streamPrefixLen)
	if _, err := io.ReadFull(src, header); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrStreamFormat, err)
	}
	if header[0] != streamVersion {
		return 0, fmt.Errorf("%w: unknown version %d", ErrStreamFormat, header[0])
	}
	prefix := header[1:]

	var (
		lenBuf  [4]byte
		sealed  = make([]byte, 0, streamChunkSize+d.aead.Overhead())
		plain   = make([]byte, 0, streamChunkSize)
		counter uint64
		total   int64
	)
	for {
		if _, err := io.ReadFull(src, lenBuf[:]); err != nil {
			// Running out of frames without ever seeing a final one means the
			// stream was cut short.
			return total, fmt.Errorf("%w: stream ended without a final frame", ErrStreamFormat)
		}
		size := int(binary.BigEndian.Uint32(lenBuf[:]))
		if size < d.aead.Overhead() || size > streamChunkSize+d.aead.Overhead() {
			return total, fmt.Errorf("%w: frame length %d", ErrStreamFormat, size)
		}
		if cap(sealed) < size {
			sealed = make([]byte, size)
		}
		sealed = sealed[:size]
		if _, err := io.ReadFull(src, sealed); err != nil {
			return total, fmt.Errorf("%w: %v", ErrStreamFormat, err)
		}

		nonce := streamNonce(prefix, counter)
		var (
			opened []byte
			err    error
			final  bool
		)
		// The trailer is authenticated, so trying both tells us which kind of
		// frame this is without trusting anything unauthenticated to say so.
		opened, err = d.aead.Open(plain[:0], nonce, sealed, []byte{frameContinues})
		if err != nil {
			opened, err = d.aead.Open(plain[:0], nonce, sealed, []byte{frameFinal})
			final = true
		}
		if err != nil {
			return total, fmt.Errorf("%w: frame %d does not authenticate", ErrCiphertext, counter)
		}
		plain = opened

		if len(opened) > 0 {
			if _, err := dst.Write(opened); err != nil {
				return total, err
			}
			total += int64(len(opened))
		}
		counter++
		if final {
			return total, nil
		}
	}
}

// readChunk fills buf and reports whether the source is exhausted.
func readChunk(src io.Reader, buf []byte) ([]byte, bool, error) {
	n, err := io.ReadFull(src, buf)
	switch {
	case err == nil:
		return buf[:n], false, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return buf[:n], true, nil
	default:
		return nil, false, err
	}
}

// streamNonce combines the stream's random prefix with the frame counter, so a
// nonce is never reused within a stream and two streams under the same key do
// not collide.
func streamNonce(prefix []byte, counter uint64) []byte {
	nonce := make([]byte, streamNonceLen)
	copy(nonce, prefix)
	binary.BigEndian.PutUint64(nonce[streamPrefixLen:], counter)
	return nonce
}

// NewContentKey mints a key for one blob's contents, returning both the usable
// key and its raw bytes so the caller can wrap it for each identity that
// receives the message.
//
// This is what reconciles two invariants that would otherwise contradict each
// other: the blob stays content-addressed and shared, while each delivery
// carries its own wrapping of the key. Purging an identity destroys its
// wrapping, so its copy becomes unreadable even though the bytes are still
// there for whoever else received the same message.
func NewContentKey() (*DataKey, []byte, error) {
	raw := make([]byte, KeySize)
	if _, err := rand.Read(raw); err != nil {
		return nil, nil, fmt.Errorf("crypto: random source unavailable: %w", err)
	}
	key, err := newDataKey(append([]byte(nil), raw...))
	if err != nil {
		return nil, nil, err
	}
	return key, raw, nil
}

// ContentKeyFrom rebuilds a content key from raw bytes recovered by unwrapping
// a delivery's stored key.
func ContentKeyFrom(raw []byte) (*DataKey, error) { return newDataKey(append([]byte(nil), raw...)) }
