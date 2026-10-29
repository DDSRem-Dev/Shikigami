package releaseauth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

func signedEnvelope(t *testing.T, private ed25519.PrivateKey, data []byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(envelope{Format: Format, Payload: base64.StdEncoding.EncodeToString(data), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, signedMessage(data)))})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestStrictEnvelopeAndPayloadRejectAmbiguity(t *testing.T) {
	for _, kind := range []string{"unknown-envelope", "duplicate-envelope", "escaped-duplicate-envelope", "alias-envelope", "trailing-envelope", "null-envelope", "future-format", "wrong-base64-payload", "newline-base64", "unpadded-base64", "noncanonical-pad-bits", "short-signature", "wrong-domain", "raw-payload-signature", "unknown-payload", "duplicate-payload", "alias-payload", "trailing-payload", "null-payload", "traversal-name", "wrong-canonical-name", "uppercase-digest", "oversize-length", "negative-length", "zero-length", "missing-key"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.signed(t)
			data := readFile(t, f.sign.Signature)
			var outer envelope
			if err := json.Unmarshal(data, &outer); err != nil {
				t.Fatal(err)
			}
			message, err := base64.StdEncoding.DecodeString(outer.Payload)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown-envelope":
				data = append([]byte(`{"public_key":"attacker controlled",`), bytes.TrimSpace(data)[1:]...)
			case "duplicate-envelope":
				data = append([]byte(`{"format":1,`), bytes.TrimSpace(data)[1:]...)
			case "escaped-duplicate-envelope":
				data = append([]byte(`{"\u0066ormat":1,`), bytes.TrimSpace(data)[1:]...)
			case "alias-envelope":
				data = bytes.Replace(data, []byte(`"format"`), []byte(`"Format"`), 1)
			case "trailing-envelope":
				data = append(data, []byte(`{}`)...)
			case "null-envelope":
				data = bytes.Replace(data, []byte(`"format":1`), []byte(`"format":null`), 1)
			case "future-format":
				outer.Format = 2
				data, _ = json.Marshal(outer)
			case "wrong-base64-payload":
				outer.Payload = "!!notbase64!!"
				data, _ = json.Marshal(outer)
			case "newline-base64":
				outer.Signature = outer.Signature[:8] + "\n" + outer.Signature[8:]
				data, _ = json.Marshal(outer)
			case "unpadded-base64":
				outer.Signature = strings.TrimRight(outer.Signature, "=")
				data, _ = json.Marshal(outer)
			case "noncanonical-pad-bits":
				// A 64-byte signature has two padding bytes. Alter only unused
				// low pad bits, which non-strict base64 decoders may discard.
				const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
				index := len(outer.Signature) - 3
				value := strings.IndexByte(alphabet, outer.Signature[index])
				outer.Signature = outer.Signature[:index] + string(alphabet[value+1]) + outer.Signature[index+1:]
				data, _ = json.Marshal(outer)
			case "short-signature":
				outer.Signature = base64.StdEncoding.EncodeToString(make([]byte, 63))
				data, _ = json.Marshal(outer)
			case "wrong-domain":
				outer.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(f.private, append([]byte("different domain"), message...)))
				data, _ = json.Marshal(outer)
			case "raw-payload-signature":
				outer.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(f.private, message))
				data, _ = json.Marshal(outer)
			default:
				var fields map[string]any
				if err := json.Unmarshal(message, &fields); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "unknown-payload":
					fields["trust_key"] = "must not trust"
				case "alias-payload":
					fields["Version"] = fields["version"]
					delete(fields, "version")
				case "null-payload":
					fields["archive_size"] = nil
				case "traversal-name":
					fields["archive"] = "../outside.tar.gz"
				case "wrong-canonical-name":
					fields["archive"] = "shikigami-v9-linux-arm64.tar.gz"
				case "uppercase-digest":
					fields["checksums_sha256"] = strings.Repeat("A", 64)
				case "oversize-length":
					fields["archive_size"] = MaxArchiveBytes + 1
				case "negative-length":
					fields["archive_size"] = -1
				case "zero-length":
					fields["archive_size"] = 0
				case "missing-key":
					delete(fields, "os")
				}
				message, _ = json.Marshal(fields)
				if kind == "duplicate-payload" {
					message = append([]byte(`{"version":"v1.2.3",`), message[1:]...)
				}
				if kind == "trailing-payload" {
					message = append(message, []byte(` []`)...)
				}
				// Authenticate the malformed payload with the legitimate fixture
				// key so schema rejection cannot be credited to a bad signature.
				data = signedEnvelope(t, f.private, message)
			}
			writeFile(t, f.sign.Signature, data, 0600)
			if result, err := Verify(context.Background(), f.verify); err == nil || result != (Verified{}) {
				t.Fatal("ambiguous or unauthenticated metadata accepted")
			}
			absent(t, f.verify.Output)
		})
	}
}

func TestPEMKeysRejectWrongTypesJunkAndSecretsInErrors(t *testing.T) {
	for _, kind := range []string{"prefix", "suffix", "multiple-blocks", "malformed-first-block", "headers", "wrong-type", "wrong-algorithm", "raw-private-material", "private-der-suffix"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			original := readFile(t, f.sign.PrivateKey)
			data := append([]byte(nil), original...)
			secret := "DO_NOT_PRINT_PRIVATE_KEY_MATERIAL_4731"
			block, _ := pem.Decode(data)
			switch kind {
			case "prefix":
				data = append([]byte(secret+"\n"), data...)
			case "suffix":
				data = append(data, []byte(secret)...)
			case "multiple-blocks":
				data = append(data, data...)
			case "malformed-first-block":
				data = append([]byte("-----BEGIN PRIVATE KEY-----\n!malformed!\n"), data...)
			case "headers":
				block.Headers = map[string]string{"Secret": secret}
				data = pem.EncodeToMemory(block)
			case "wrong-type":
				block.Type = "ED25519 PRIVATE KEY"
				data = pem.EncodeToMemory(block)
			case "wrong-algorithm":
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				block.Bytes, err = x509.MarshalPKCS8PrivateKey(key)
				if err != nil {
					t.Fatal(err)
				}
				data = pem.EncodeToMemory(block)
			case "raw-private-material":
				block.Bytes = append([]byte(nil), f.private...)
				block.Bytes[63] ^= 1
				data = pem.EncodeToMemory(block)
			case "private-der-suffix":
				block.Bytes = append(block.Bytes, []byte(secret)...)
				data = pem.EncodeToMemory(block)
			}
			writeFile(t, f.sign.PrivateKey, data, 0600)
			err := Sign(context.Background(), f.sign)
			if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(f.private)) || strings.Contains(err.Error(), string(data)) {
				t.Fatal("invalid private key accepted or key material disclosed")
			}
			absent(t, f.sign.Signature)
		})
	}
	for _, kind := range []string{"private-as-public", "wrong-algorithm", "multiple-blocks", "malformed-first-block", "prefix", "suffix", "headers", "der-suffix"} {
		t.Run("public-"+kind, func(t *testing.T) {
			f := newFixture(t)
			f.signed(t)
			data := readFile(t, f.verify.PublicKey)
			block, _ := pem.Decode(data)
			switch kind {
			case "private-as-public":
				data = readFile(t, f.sign.PrivateKey)
			case "wrong-algorithm":
				key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				block.Bytes, _ = x509.MarshalPKIXPublicKey(&key.PublicKey)
				data = pem.EncodeToMemory(block)
			case "multiple-blocks":
				data = append(data, data...)
			case "malformed-first-block":
				data = append([]byte("-----BEGIN PUBLIC KEY-----\n!malformed!\n"), data...)
			case "prefix":
				data = append([]byte("not PEM\n"), data...)
			case "suffix":
				data = append(data, []byte("not PEM")...)
			case "headers":
				block.Headers = map[string]string{"Comment": "unsupported"}
				data = pem.EncodeToMemory(block)
			case "der-suffix":
				block.Bytes = append(block.Bytes, 0)
				data = pem.EncodeToMemory(block)
			}
			writeFile(t, f.verify.PublicKey, data, 0644)
			if _, err := Verify(context.Background(), f.verify); err == nil {
				t.Fatal("invalid trust key accepted")
			}
			absent(t, f.verify.Output)
		})
	}
}
