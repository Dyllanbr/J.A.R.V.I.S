package application

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
)

// RequestFingerprint is the SHA-256 digest of canonical, normalized command
// semantics. It contains no raw request representation.
type RequestFingerprint [sha256.Size]byte

func newRequestFingerprintDigest() hash.Hash {
	return sha256.New()
}

func writeFingerprintString(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

func writeFingerprintInt64(digest hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = digest.Write(encoded[:])
}
